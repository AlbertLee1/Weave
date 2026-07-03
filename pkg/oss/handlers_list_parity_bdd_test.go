package oss_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss"
)

// listBDDDecodeRows decodes an ObjectPage's flattened rows or fails the test
// when the status is not 200.
func listBDDDecodeRows(t *testing.T, rec *httptest.ResponseRecorder) []map[string]interface{} {
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

// TestBDD_ListObjects_SelectProjection_FoundryParity locks the Foundry
// `?select=` list projection contract on GET .../objects/{objectType}.
//
// Foundry contract: the list endpoint accepts a `select` array query parameter
// (repeated `?select=a&select=b` or the CSV shorthand `?select=a,b`). When
// present the response objects retain ONLY the named property apiNames (plus
// the primary-key field and the reserved system keys). When absent the full
// property set is returned — the search-body select (#306) already does this,
// but the list GET never carried it, so official OSDK list calls asking for a
// narrow projection got HTTP 200 with every property (silent over-fetch).
//
// Acceptance criteria (Given -> When -> Then):
//
//	Given seeded employees emp1/emp2/emp3 each with name+age+active+deptId
//	When  list runs with ?select=name
//	Then  each row carries ONLY name + employeeId + reserved system keys
//
//	Given the same seed
//	When  list runs with ?select=name,age (CSV) or ?select=name&select=age
//	Then  each row carries name + age + employeeId, but not active/deptId
//
//	Given the same seed
//	When  select is omitted
//	Then  the full property set comes back (select is optional)
func TestBDD_ListObjects_SelectProjection_FoundryParity(t *testing.T) {
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

	assertPresent := func(t *testing.T, row map[string]interface{}, keys ...string) {
		t.Helper()
		for _, k := range keys {
			if _, ok := row[k]; !ok {
				t.Errorf("row missing expected key %q; row=%v", k, row)
			}
		}
	}
	assertAbsent := func(t *testing.T, row map[string]interface{}, keys ...string) {
		t.Helper()
		for _, k := range keys {
			if _, ok := row[k]; ok {
				t.Errorf("row must not carry key %q; row=%v", k, row)
			}
		}
	}

	t.Run("select single property projects to it + primary key only", func(t *testing.T) {
		rows := listBDDDecodeRows(t, doList(t, "select=name"))
		if len(rows) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(rows))
		}
		for _, row := range rows {
			assertPresent(t, row, "name", "employeeId", "__primaryKey", "__apiName")
			assertAbsent(t, row, "age", "active", "deptId")
		}
	})

	t.Run("select CSV projects to the listed properties", func(t *testing.T) {
		rows := listBDDDecodeRows(t, doList(t, "select=name,age"))
		for _, row := range rows {
			assertPresent(t, row, "name", "age", "employeeId", "__primaryKey", "__apiName")
			assertAbsent(t, row, "active", "deptId")
		}
	})

	t.Run("repeated select params project to the union", func(t *testing.T) {
		rows := listBDDDecodeRows(t, doList(t, "select=name&select=age"))
		for _, row := range rows {
			assertPresent(t, row, "name", "age", "employeeId", "__primaryKey", "__apiName")
			assertAbsent(t, row, "active", "deptId")
		}
	})

	t.Run("omitted select returns the full property set", func(t *testing.T) {
		rows := listBDDDecodeRows(t, doList(t, ""))
		for _, row := range rows {
			assertPresent(t, row, "employeeId", "name", "age", "active", "deptId", "__primaryKey", "__apiName")
		}
	})
}

// TestBDD_ListObjects_DefaultPageSize_FoundryParity locks the Foundry list
// default page size (1000) on GET .../objects/{objectType}. Before this change
// the list endpoint inherited the shared 100 default, so a list of >100 objects
// was silently truncated to 100 rows behind a cursor, diverging from Foundry
// where the list default returns up to 1000.
//
// Acceptance criteria (Given -> When -> Then):
//
//	Given 150 seeded employees
//	When  list runs with no pageSize
//	Then  all 150 rows come back in a single page with no nextPageToken
//
//	Given the same seed
//	When  list runs with an explicit ?pageSize=50
//	Then  exactly 50 rows come back plus a nextPageToken (explicit wins)
func TestBDD_ListObjects_DefaultPageSize_FoundryParity(t *testing.T) {
	const n = 150
	svc := seedListParityEmployees(t, n)
	h := oss.NewHandler(svc)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	doList := func(t *testing.T, query string) *httptest.ResponseRecorder {
		t.Helper()
		url := "/api/v2/ontologies/" + testOntologyRID + "/objects/employee"
		if query != "" {
			url += "?" + query
		}
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	decodePage := func(t *testing.T, rec *httptest.ResponseRecorder) (int, string) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var page struct {
			Data          []map[string]interface{} `json:"data"`
			NextPageToken string                   `json:"nextPageToken"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
		}
		return len(page.Data), page.NextPageToken
	}

	t.Run("no pageSize returns up to 1000 in one page", func(t *testing.T) {
		count, next := decodePage(t, doList(t, ""))
		if count != n {
			t.Fatalf("default list returned %d rows, want %d (Foundry list default is 1000)", count, n)
		}
		if next != "" {
			t.Errorf("all %d rows fit under the 1000 default; want no nextPageToken, got %q", n, next)
		}
	})

	t.Run("explicit pageSize still honored", func(t *testing.T) {
		count, next := decodePage(t, doList(t, "pageSize=50"))
		if count != 50 {
			t.Fatalf("explicit pageSize=50 returned %d rows, want 50", count)
		}
		if next == "" {
			t.Errorf("50-of-%d must page; want a nextPageToken, got none", n)
		}
	})
}

// TestBDD_ListObjects_OrderByPropertiesPrefix_FoundryParity locks the Foundry
// list orderBy form `properties.{apiName}:{dir}` on GET .../objects/{objectType},
// while proving the legacy bare `{field}:{dir}` form still works.
//
// Acceptance criteria (Given -> When -> Then):
//
//	Given seeded employees alice(30)/bob(25)/charlie(35)
//	When  list runs with ?orderBy=properties.age:desc
//	Then  rows come back age-descending: emp3, emp1, emp2
//
//	Given the same seed
//	When  list runs with the legacy bare ?orderBy=age:desc
//	Then  the same order comes back (backward compatible)
func TestBDD_ListObjects_OrderByPropertiesPrefix_FoundryParity(t *testing.T) {
	orderedPKs := func(t *testing.T, query string) []string {
		t.Helper()
		svc, _, _, _ := setupOSSTest(t)
		h := oss.NewHandler(svc)
		r := chi.NewRouter()
		h.RegisterRoutes(r)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v2/ontologies/"+testOntologyRID+"/objects/employee?"+query, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		rows := listBDDDecodeRows(t, rec)
		pks := make([]string, 0, len(rows))
		for _, row := range rows {
			pk, _ := row["__primaryKey"].(string)
			pks = append(pks, pk)
		}
		return pks
	}

	assertOrder := func(t *testing.T, got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("primary keys = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("primary keys = %v, want %v (order matters)", got, want)
			}
		}
	}

	t.Run("properties.age:desc sorts descending", func(t *testing.T) {
		assertOrder(t, orderedPKs(t, "orderBy=properties.age:desc"), "emp3", "emp1", "emp2")
	})

	t.Run("legacy bare age:desc still sorts descending", func(t *testing.T) {
		assertOrder(t, orderedPKs(t, "orderBy=age:desc"), "emp3", "emp1", "emp2")
	})

	t.Run("properties.age:asc sorts ascending", func(t *testing.T) {
		assertOrder(t, orderedPKs(t, "orderBy=properties.age:asc"), "emp2", "emp1", "emp3")
	})
}
