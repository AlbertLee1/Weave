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

// TestBDD_SearchObjects_ExcludeRID_FoundryParity locks the Foundry
// SearchObjectsRequestV2.excludeRid contract on
// POST .../objects/{objectType}/search.
//
// Foundry contract: when the request body sets `excludeRid: true`, the
// response objects are returned WITHOUT their reserved `__rid` key. When the
// field is absent / false the objects carry `__rid` as before. This is a
// pure back-compat overlay — the default response shape is unchanged.
//
// Acceptance criteria (Given → When → Then):
//
//	Given seeded employees emp1/emp2/emp3
//	When  search runs with {"excludeRid": true}
//	Then  every returned object omits __rid but keeps __primaryKey/__apiName
//	      and its property fields
//
//	Given the same seed
//	When  excludeRid is omitted
//	Then  every returned object carries __rid (default preserved)
func TestBDD_SearchObjects_ExcludeRID_FoundryParity(t *testing.T) {
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

	t.Run("excludeRid true strips __rid but keeps other reserved keys", func(t *testing.T) {
		rec := doSearch(t, `{"excludeRid":true}`)
		rows := decodeRows(t, rec)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d; body=%s", len(rows), rec.Body.String())
		}
		for _, row := range rows {
			if _, ok := row["__rid"]; ok {
				t.Errorf("excludeRid=true must omit __rid; row=%v", row)
			}
			// The other reserved keys and property fields must survive.
			for _, k := range []string{"__primaryKey", "__apiName", "employeeId", "name"} {
				if _, ok := row[k]; !ok {
					t.Errorf("row missing %q under excludeRid=true; row=%v", k, row)
				}
			}
		}
	})

	t.Run("omitted excludeRid keeps __rid (default)", func(t *testing.T) {
		rec := doSearch(t, `{}`)
		rows := decodeRows(t, rec)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d; body=%s", len(rows), rec.Body.String())
		}
		for _, row := range rows {
			if _, ok := row["__rid"]; !ok {
				t.Errorf("default search must keep __rid; row=%v", row)
			}
		}
	})

	t.Run("excludeRid false keeps __rid", func(t *testing.T) {
		rec := doSearch(t, `{"excludeRid":false}`)
		rows := decodeRows(t, rec)
		for _, row := range rows {
			if _, ok := row["__rid"]; !ok {
				t.Errorf("excludeRid=false must keep __rid; row=%v", row)
			}
		}
	})
}

// TestBDD_ListObjects_ExcludeRID_FoundryParity locks the Foundry
// `?excludeRid=true` list query-parameter contract on
// GET .../objects/{objectType}.
//
// Acceptance criteria (Given → When → Then):
//
//	Given seeded employees emp1/emp2/emp3
//	When  list runs with ?excludeRid=true
//	Then  every returned object omits __rid
//
//	Given the same seed
//	When  excludeRid is absent
//	Then  every returned object carries __rid (default preserved)
func TestBDD_ListObjects_ExcludeRID_FoundryParity(t *testing.T) {
	doList := func(t *testing.T, query string) *httptest.ResponseRecorder {
		t.Helper()
		svc, _, _, _ := setupOSSTest(t)
		h := oss.NewHandler(svc)
		r := chi.NewRouter()
		h.RegisterRoutes(r)

		url := "/api/v2/ontologies/" + testOntologyRID + "/objects/employee"
		if query != "" {
			url += "?" + query
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
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

	t.Run("excludeRid=true strips __rid", func(t *testing.T) {
		rec := doList(t, "excludeRid=true")
		rows := decodeRows(t, rec)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d; body=%s", len(rows), rec.Body.String())
		}
		for _, row := range rows {
			if _, ok := row["__rid"]; ok {
				t.Errorf("excludeRid=true must omit __rid; row=%v", row)
			}
			for _, k := range []string{"__primaryKey", "__apiName", "employeeId"} {
				if _, ok := row[k]; !ok {
					t.Errorf("row missing %q under excludeRid=true; row=%v", k, row)
				}
			}
		}
	})

	t.Run("absent excludeRid keeps __rid (default)", func(t *testing.T) {
		rec := doList(t, "")
		rows := decodeRows(t, rec)
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d; body=%s", len(rows), rec.Body.String())
		}
		for _, row := range rows {
			if _, ok := row["__rid"]; !ok {
				t.Errorf("default list must keep __rid; row=%v", row)
			}
		}
	})
}
