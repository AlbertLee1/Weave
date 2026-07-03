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

// TestBDD_SearchObjects_SelectProjection_FoundryParity locks the Foundry
// SearchObjectsRequestV2.select projection contract on
// POST .../objects/{objectType}/search.
//
// Foundry contract: `select` names the property apiNames the response should
// carry. Before this change the handler VALIDATED select (rejecting an empty
// list with 400 SelectRequired) but the service never applied it — every
// response returned the full property set regardless of select. That is the
// silent-correctness bug class: official OSDK clients that ask for a narrow
// projection got HTTP 200 with every property and no way to notice the
// over-fetch (and the extra columns can leak data the caller deliberately
// scoped out).
//
// Acceptance criteria (Given → When → Then):
//
//	Given seeded employees emp1/emp2/emp3 each with name+age+active+deptId
//	When  search runs with select ["name"]
//	Then  each returned object carries ONLY name + the primary key
//	      (employeeId) + reserved system keys — age/active/deptId are gone
//
//	Given the same seed
//	When  select is omitted entirely
//	Then  the full property set comes back (select is optional, Foundry-style)
func TestBDD_SearchObjects_SelectProjection_FoundryParity(t *testing.T) {
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

	decodeRows := func(t *testing.T, rec *httptest.ResponseRecorder) []map[string]interface{} {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var page struct {
			Data []map[string]interface{} `json:"data"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
		}
		return page.Data
	}

	t.Run("select subset projects to selected + primary key only", func(t *testing.T) {
		rec := doSearch(t, `{"select":["name"]}`)
		rows := decodeRows(t, rec)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d; body=%s", len(rows), rec.Body.String())
		}
		for _, row := range rows {
			// Selected property, primary key and reserved keys survive.
			for _, k := range []string{"name", "employeeId", "__primaryKey", "__apiName"} {
				if _, ok := row[k]; !ok {
					t.Errorf("row missing %q under select [name]; row=%v", k, row)
				}
			}
			// Every unselected, non-key property is stripped.
			for _, k := range []string{"age", "active", "deptId"} {
				if _, ok := row[k]; ok {
					t.Errorf("row must not carry unselected %q; row=%v", k, row)
				}
			}
		}
	})

	t.Run("omitted select returns the full property set", func(t *testing.T) {
		rec := doSearch(t, `{}`)
		rows := decodeRows(t, rec)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d; body=%s", len(rows), rec.Body.String())
		}
		for _, row := range rows {
			for _, k := range []string{"employeeId", "name", "age", "active", "deptId", "__primaryKey", "__apiName"} {
				if _, ok := row[k]; !ok {
					t.Errorf("full response missing %q; row=%v", k, row)
				}
			}
		}
	})
}
