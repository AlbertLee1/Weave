package oss_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss"
)

// TestBDD_SearchObjects_InOperator_FoundryParity covers the Foundry
// SearchJsonQueryV2 parity gap on POST .../objects/{objectType}/search:
// the `in` filter type.
//
// Foundry contract: {"type":"in","field":"<prop>","value":[v1,v2,...]}
// matches objects whose field equals ANY value in the array — the exact
// equivalent of OR-ing one "eq" clause per element. This is the highest
// frequency OSDK filter Weave was missing; before this change the
// converter's default branch rejected it with HTTP 400
// `unsupported where clause type: "in"`.
//
// Acceptance criteria (Given → When → Then):
//
//	Given seeded employees alice(30,d1) / bob(25,d1) / charlie(35,d2)
//	When  SearchObjects runs with where {"type":"in","field":"name",
//	      "value":["alice","charlie"]}
//	Then  it returns HTTP 200 with exactly emp1 and emp3
//
//	Given the same seed
//	When  the `in` array carries numeric values [25]
//	Then  only bob (emp2) matches — numeric elements keep eq's numeric
//	      range semantics instead of being coerced to strings
//
//	Given an EMPTY `in` array
//	When  the search runs
//	Then  it is a legal query that matches zero objects (HTTP 200, data=[])
//
//	Given a NON-ARRAY `in` value
//	When  the search runs
//	Then  it returns HTTP 400 with errorName "InvalidWhereClause" and a
//	      reason explaining the array requirement
func TestBDD_SearchObjects_InOperator_FoundryParity(t *testing.T) {
	doSearch := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		svc, _, _, _ := setupOSSTest(t)
		h := oss.NewHandler(svc)
		r := chi.NewRouter()
		h.RegisterRoutes(r)

		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/search",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	primaryKeys := func(t *testing.T, rec *httptest.ResponseRecorder) []string {
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
		sort.Strings(pks)
		return pks
	}

	t.Run("in over string values returns only objects matching any candidate", func(t *testing.T) {
		rec := doSearch(t, `{"select":["name"],"where":{"type":"in","field":"name","value":["alice","charlie"]}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got := primaryKeys(t, rec)
		want := []string{"emp1", "emp3"}
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("primary keys = %v, want %v", got, want)
		}
	})

	t.Run("in over numeric values keeps eq numeric semantics", func(t *testing.T) {
		rec := doSearch(t, `{"select":["name"],"where":{"type":"in","field":"age","value":[25]}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		got := primaryKeys(t, rec)
		if len(got) != 1 || got[0] != "emp2" {
			t.Errorf("primary keys = %v, want [emp2]", got)
		}
	})

	t.Run("empty in array is a legal zero-match query", func(t *testing.T) {
		rec := doSearch(t, `{"select":["name"],"where":{"type":"in","field":"name","value":[]}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if got := primaryKeys(t, rec); len(got) != 0 {
			t.Errorf("primary keys = %v, want [] (empty candidate list matches nothing)", got)
		}
	})

	t.Run("non-array in value returns 400 InvalidWhereClause", func(t *testing.T) {
		rec := doSearch(t, `{"select":["name"],"where":{"type":"in","field":"name","value":"alice"}}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		var env struct {
			ErrorCode  string            `json:"errorCode"`
			ErrorName  string            `json:"errorName"`
			Parameters map[string]string `json:"parameters"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		if env.ErrorCode != "INVALID_ARGUMENT" {
			t.Errorf("errorCode = %q, want INVALID_ARGUMENT", env.ErrorCode)
		}
		if env.ErrorName != "InvalidWhereClause" {
			t.Errorf("errorName = %q, want InvalidWhereClause", env.ErrorName)
		}
		if !strings.Contains(env.Parameters["reason"], "in value must be an array") {
			t.Errorf("parameters.reason = %q, want it to explain the array requirement", env.Parameters["reason"])
		}
	})
}
