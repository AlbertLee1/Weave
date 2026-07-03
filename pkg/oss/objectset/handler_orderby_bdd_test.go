package objectset_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// TestBDD_LoadObjects_BodyOrderBy_FoundryParity covers the Foundry
// LoadObjectSetRequestV2.orderBy parity gap on
// POST .../objectSets/loadObjects.
//
// Foundry contract: orderBy is the same SearchOrderByV2 shape the search
// endpoint takes — {"fields":[{"field":"<prop>","direction":"asc"|"desc"}]}
// with direction defaulting to "asc". Before this change the handler parsed
// req.OrderBy and never read it: results came back in the executor's PK
// order with HTTP 200, so SDK callers (including Weave's own web frontend,
// which already sends this exact body from useLoadObjectSet) silently
// received unsorted pages.
//
// Acceptance criteria (Given → When → Then):
//
//	Given 5 seeded employees with distinct numeric ranks
//	When  loadObjects runs with orderBy rank desc and pageSize 2
//	Then  every page is rank-descending AND the pages concatenate into the
//	      full descending sequence (sorting happens BEFORE pagination, so
//	      the order is stable across pages — no duplicates, no gaps)
//
//	Given the same seed
//	When  orderBy omits direction
//	Then  ascending is the default
//
//	Given orderType "relevance"
//	When  loadObjects runs
//	Then  HTTP 400 InvalidOrderBy — an ObjectSet resolves to explicit
//	      primary keys where every doc scores identically, so relevance
//	      would be a silent no-op; we reject instead of degrading
//
//	Given an invalid direction
//	When  loadObjects runs
//	Then  HTTP 400 InvalidOrderBy
//
//	Given orderBy combined with ?asOf= time travel
//	When  loadObjects runs
//	Then  HTTP 400 OrderByUnsupportedWithAsOf — the snapshot path has no
//	      Bleve index to sort against; explicit rejection, never a silent
//	      PK-order fallback
func TestBDD_LoadObjects_BodyOrderBy_FoundryParity(t *testing.T) {
	setup := func(t *testing.T) http.Handler {
		t.Helper()
		dir := t.TempDir()
		mgr := index.NewManager(dir)
		t.Cleanup(func() { mgr.Close() })

		props := []index.Property{
			{APIName: "id", BaseType: "string", IsSearchable: true},
			{APIName: "name", BaseType: "string", IsSearchable: true},
			{APIName: "rank", BaseType: "integer", IsSearchable: true},
		}
		if _, err := mgr.EnsureIndex("employee", props); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}

		docs := []struct {
			id   string
			rank float64
		}{
			{"e1", 10}, {"e2", 40}, {"e3", 20}, {"e4", 50}, {"e5", 30},
		}
		for _, d := range docs {
			doc := map[string]interface{}{"id": d.id, "name": "n-" + d.id, "rank": d.rank}
			if err := mgr.IndexDocument("employee", d.id, doc); err != nil {
				t.Fatalf("IndexDocument %s: %v", d.id, err)
			}
		}
		// Let the index settle like the other handler tests do.
		time.Sleep(200 * time.Millisecond)

		store := objectset.NewStore(1 * time.Hour)
		executor := objectset.NewExecutor(mgr, &mockLinkResolverWithType{}, store)
		handler := objectset.NewHandler(executor, mgr, store)

		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/objectSets/loadObjects", handler.LoadObjects)
		return r
	}

	load := func(t *testing.T, router http.Handler, query, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/myOntology/objectSets/loadObjects"+query,
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	type loadPage struct {
		Data []struct {
			PrimaryKey string `json:"__primaryKey"`
		} `json:"data"`
		NextPageToken string `json:"nextPageToken"`
		TotalCount    string `json:"totalCount"`
	}

	decodePage := func(t *testing.T, rec *httptest.ResponseRecorder) loadPage {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var page loadPage
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
		}
		return page
	}

	assertInvalid := func(t *testing.T, rec *httptest.ResponseRecorder, wantName string) {
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
		if env.ErrorName != wantName {
			t.Errorf("errorName = %q, want %s", env.ErrorName, wantName)
		}
	}

	t.Run("orderBy desc is applied before pagination and stable across pages", func(t *testing.T) {
		router := setup(t)
		want := []string{"e4", "e2", "e5", "e3", "e1"} // rank 50,40,30,20,10

		var got []string
		pageToken := ""
		for pages := 0; ; pages++ {
			if pages > 4 {
				t.Fatalf("pagination did not terminate; collected %v", got)
			}
			body := `{"objectSet":{"type":"base","objectType":"employee"},"select":["id","rank"],"pageSize":2,` +
				`"orderBy":{"fields":[{"field":"rank","direction":"desc"}]}`
			if pageToken != "" {
				body += `,"pageToken":"` + pageToken + `"`
			}
			body += `}`
			page := decodePage(t, load(t, router, "", body))
			if len(page.Data) > 2 {
				t.Fatalf("page has %d items, want <= pageSize 2", len(page.Data))
			}
			for _, d := range page.Data {
				got = append(got, d.PrimaryKey)
			}
			if page.NextPageToken == "" {
				break
			}
			pageToken = page.NextPageToken
		}

		if len(got) != len(want) {
			t.Fatalf("concatenated pages = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("concatenated pages = %v, want %v (cross-page order must be stable)", got, want)
			}
		}
	})

	t.Run("omitted direction defaults to ascending", func(t *testing.T) {
		router := setup(t)
		page := decodePage(t, load(t, router, "",
			`{"objectSet":{"type":"base","objectType":"employee"},"select":["id","rank"],"orderBy":{"fields":[{"field":"rank"}]}}`))
		want := []string{"e1", "e3", "e5", "e2", "e4"} // rank 10,20,30,40,50
		if len(page.Data) != len(want) {
			t.Fatalf("data length = %d, want %d; body pks=%+v", len(page.Data), len(want), page.Data)
		}
		for i := range want {
			if page.Data[i].PrimaryKey != want[i] {
				got := make([]string, len(page.Data))
				for j, d := range page.Data {
					got[j] = d.PrimaryKey
				}
				t.Fatalf("data order = %v, want %v", got, want)
			}
		}
	})

	t.Run("orderType relevance is 400 InvalidOrderBy on loadObjects", func(t *testing.T) {
		router := setup(t)
		rec := load(t, router, "",
			`{"objectSet":{"type":"base","objectType":"employee"},"select":["id"],"orderBy":{"orderType":"relevance","fields":[]}}`)
		assertInvalid(t, rec, "InvalidOrderBy")
	})

	t.Run("invalid direction is 400 InvalidOrderBy", func(t *testing.T) {
		router := setup(t)
		rec := load(t, router, "",
			`{"objectSet":{"type":"base","objectType":"employee"},"select":["id"],"orderBy":{"fields":[{"field":"rank","direction":"down"}]}}`)
		assertInvalid(t, rec, "InvalidOrderBy")
	})

	t.Run("orderBy with asOf is rejected explicitly, not silently dropped", func(t *testing.T) {
		router := setup(t)
		rec := load(t, router, "?asOf=2026-01-01T00:00:00Z",
			`{"objectSet":{"type":"base","objectType":"employee"},"select":["id"],"orderBy":{"fields":[{"field":"rank"}]}}`)
		assertInvalid(t, rec, "OrderByUnsupportedWithAsOf")
	})
}
