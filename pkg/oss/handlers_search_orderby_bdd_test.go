package oss_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss"
)

// TestBDD_SearchObjects_BodyOrderBy_FoundryParity covers the Foundry
// SearchObjectsRequestV2.orderBy parity gap on
// POST .../objects/{objectType}/search.
//
// Foundry contract (SearchOrderByV2):
//
//	{"orderType": "fields"|"relevance" (optional, default "fields"),
//	 "fields": [{"field": "<prop>", "direction": "asc"|"desc"}]}
//
// direction defaults to "asc" when omitted. Before this change the handler
// parsed body.orderBy into a struct and then never read it — only the legacy
// `?orderBy=field:desc` query param was honoured — so official OSDK /
// foundry-platform-python clients got HTTP 200 with UNSORTED data and no way
// to notice (the silent-correctness bug class).
//
// Acceptance criteria (Given → When → Then):
//
//	Given seeded employees alice(30) / bob(25) / charlie(35)
//	When  search runs with body orderBy {fields:[{field:age,direction:desc}]}
//	Then  data comes back strictly age-descending: emp3, emp1, emp2
//
//	Given the same seed
//	When  the body orderBy omits direction entirely
//	Then  ascending is the default: emp2, emp1, emp3
//
//	Given both body orderBy (desc) AND legacy ?orderBy=age:asc
//	When  the search runs
//	Then  the body — the documented Foundry V2 form — wins: descending
//
//	Given orderType "relevance" with no fields and a full-text where
//	When  the search runs
//	Then  HTTP 200 (Bleve score sort, "-_score")
//
//	Given orderType "relevance" combined with a non-empty fields array,
//	      an unknown orderType, or an invalid direction
//	When  the search runs
//	Then  HTTP 400 with errorName "InvalidOrderBy" — never a silent
//	      fall-back to unsorted results
func TestBDD_SearchObjects_BodyOrderBy_FoundryParity(t *testing.T) {
	doSearch := func(t *testing.T, query, body string) *httptest.ResponseRecorder {
		t.Helper()
		svc, _, _, _ := setupOSSTest(t)
		h := oss.NewHandler(svc)
		r := chi.NewRouter()
		h.RegisterRoutes(r)

		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/search"+query,
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	// orderedPrimaryKeys preserves response order — the whole point of the
	// scenario — unlike the sorted helper other search BDD tests use.
	orderedPrimaryKeys := func(t *testing.T, rec *httptest.ResponseRecorder) []string {
		t.Helper()
		var page struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
		}
		pks := make([]string, 0, len(page.Data))
		for _, obj := range page.Data {
			pk, _ := obj["__primaryKey"].(string)
			pks = append(pks, pk)
		}
		return pks
	}

	assertOrder := func(t *testing.T, rec *httptest.ResponseRecorder, want ...string) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got := orderedPrimaryKeys(t, rec)
		if len(got) != len(want) {
			t.Fatalf("primary keys = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("primary keys = %v, want %v (order matters)", got, want)
			}
		}
	}

	assertInvalidOrderBy := func(t *testing.T, rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorCode string `json:"errorCode"`
			ErrorName string `json:"errorName"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		if env.ErrorCode != "INVALID_ARGUMENT" {
			t.Errorf("errorCode = %q, want INVALID_ARGUMENT", env.ErrorCode)
		}
		if env.ErrorName != "InvalidOrderBy" {
			t.Errorf("errorName = %q, want InvalidOrderBy", env.ErrorName)
		}
	}

	t.Run("body orderBy desc sorts data descending", func(t *testing.T) {
		rec := doSearch(t, "",
			`{"select":["name"],"orderBy":{"fields":[{"field":"age","direction":"desc"}]}}`)
		assertOrder(t, rec, "emp3", "emp1", "emp2")
	})

	t.Run("omitted direction defaults to ascending", func(t *testing.T) {
		rec := doSearch(t, "",
			`{"select":["name"],"orderBy":{"fields":[{"field":"age"}]}}`)
		assertOrder(t, rec, "emp2", "emp1", "emp3")
	})

	t.Run("explicit orderType fields is accepted", func(t *testing.T) {
		rec := doSearch(t, "",
			`{"select":["name"],"orderBy":{"orderType":"fields","fields":[{"field":"age","direction":"asc"}]}}`)
		assertOrder(t, rec, "emp2", "emp1", "emp3")
	})

	t.Run("body orderBy wins over legacy query param", func(t *testing.T) {
		rec := doSearch(t, "?orderBy=age:asc",
			`{"select":["name"],"orderBy":{"fields":[{"field":"age","direction":"desc"}]}}`)
		assertOrder(t, rec, "emp3", "emp1", "emp2")
	})

	t.Run("legacy query param still works when body has no orderBy", func(t *testing.T) {
		rec := doSearch(t, "?orderBy=age:desc", `{"select":["name"]}`)
		assertOrder(t, rec, "emp3", "emp1", "emp2")
	})

	t.Run("orderType relevance sorts by score", func(t *testing.T) {
		rec := doSearch(t, "",
			`{"select":["name"],"where":{"type":"eq","field":"name","value":"alice"},"orderBy":{"orderType":"relevance","fields":[]}}`)
		assertOrder(t, rec, "emp1")
	})

	t.Run("orderType relevance with fields is 400 InvalidOrderBy", func(t *testing.T) {
		rec := doSearch(t, "",
			`{"select":["name"],"orderBy":{"orderType":"relevance","fields":[{"field":"age"}]}}`)
		assertInvalidOrderBy(t, rec)
	})

	t.Run("unknown orderType is 400 InvalidOrderBy", func(t *testing.T) {
		rec := doSearch(t, "",
			`{"select":["name"],"orderBy":{"orderType":"score","fields":[]}}`)
		assertInvalidOrderBy(t, rec)
	})

	t.Run("invalid direction is 400 InvalidOrderBy", func(t *testing.T) {
		rec := doSearch(t, "",
			`{"select":["name"],"orderBy":{"fields":[{"field":"age","direction":"descending"}]}}`)
		assertInvalidOrderBy(t, rec)
	})

	t.Run("empty field name is 400 InvalidOrderBy", func(t *testing.T) {
		rec := doSearch(t, "",
			`{"select":["name"],"orderBy":{"fields":[{"field":"","direction":"asc"}]}}`)
		assertInvalidOrderBy(t, rec)
	})
}
