package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_QueryTypeAdminCRUD closes the last missing admin-CRUD surface on
// the OMS metadata family. QueryType is a first-class ontology entity, but
// its Create/Update/Delete routes were never mounted in cmd/server/routes.go
// even though the handlers (CreateQueryType / UpdateQueryType /
// DeleteQueryType) and repo methods were fully written. ActionType already
// exposes the byRid admin-CRUD style:
//
//	POST   /api/v2/ontologies/{ontologyApiName}/queryTypes
//	PUT    /api/v2/ontologies/{ontologyApiName}/queryTypes/byRid/{queryTypeRid}
//	DELETE /api/v2/ontologies/{ontologyApiName}/queryTypes/byRid/{queryTypeRid}
//
// Scenario (Given/When/Then), exercising the same router wiring used in
// production (cmd/server/routes.go):
//
//	Given an ontology with no query types
//	When  a client POSTs a new QueryType
//	Then  it is created (201) and retrievable via GET
//	When  the client PUTs an update to displayName/description
//	Then  the update persists and is visible on the next GET
//	When  the client DELETEs the QueryType
//	Then  it returns 204 and a subsequent GET returns 404
func TestBDD_QueryTypeAdminCRUD(t *testing.T) {
	const ontAPIName = "northwind"
	const ontRID = "ri.ontology.main.ontology.nw"

	newServer := func(t *testing.T) (*chi.Mux, *mockRepo) {
		t.Helper()
		repo := &mockRepo{}
		repo.ontologies = append(repo.ontologies, oms.Ontology{
			RID:         ontRID,
			APIName:     ontAPIName,
			DisplayName: "Northwind",
		})
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		// Mirror exactly the routes registered in cmd/server/routes.go so the
		// BDD test pins the path/param-name contract the handlers depend on.
		r.Post("/api/v2/ontologies/{ontologyApiName}/queryTypes", handler.CreateQueryType)
		r.Get("/api/v2/ontologies/{ontologyApiName}/queryTypes/byRid/{queryTypeRid}", handler.GetQueryType)
		r.Put("/api/v2/ontologies/{ontologyApiName}/queryTypes/byRid/{queryTypeRid}", handler.UpdateQueryType)
		r.Delete("/api/v2/ontologies/{ontologyApiName}/queryTypes/byRid/{queryTypeRid}", handler.DeleteQueryType)
		return r, repo
	}

	do := func(t *testing.T, r *chi.Mux, method, path string, body any) *httptest.ResponseRecorder {
		t.Helper()
		var reader *bytes.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("create -> get -> update -> delete -> 404", func(t *testing.T) {
		r, _ := newServer(t)
		base := "/api/v2/ontologies/" + ontAPIName + "/queryTypes"

		// --- CREATE ---
		createBody := map[string]any{
			"apiName":     "topCustomers",
			"displayName": "Top Customers",
			"description": "Customers ranked by total spend",
			"parameters":  []map[string]any{{"name": "limit", "dataType": map[string]any{"type": "integer"}}},
			"output":      map[string]any{"type": "array"},
			"query":       map[string]any{"kind": "aggregation"},
		}
		rec := do(t, r, http.MethodPost, base, createBody)
		if rec.Code != http.StatusCreated {
			t.Fatalf("CREATE status=%d, want 201; body=%s", rec.Code, rec.Body.String())
		}
		var created map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("CREATE: invalid JSON: %v", err)
		}
		rid, _ := created["rid"].(string)
		if rid == "" {
			t.Fatalf("CREATE: missing rid in response; body=%s", rec.Body.String())
		}
		if created["apiName"] != "topCustomers" {
			t.Errorf("CREATE: apiName=%v, want topCustomers", created["apiName"])
		}

		// --- GET (round-trips) ---
		rec = do(t, r, http.MethodGet, base+"/byRid/"+rid, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var got map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["displayName"] != "Top Customers" {
			t.Errorf("GET: displayName=%v, want 'Top Customers'", got["displayName"])
		}

		// --- UPDATE ---
		updateBody := map[string]any{
			"displayName": "Top Customers (Revised)",
			"description": "Updated ranking",
			"status":      "ACTIVE",
		}
		rec = do(t, r, http.MethodPut, base+"/byRid/"+rid, updateBody)
		if rec.Code != http.StatusOK {
			t.Fatalf("UPDATE status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var updated map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &updated)
		if updated["displayName"] != "Top Customers (Revised)" {
			t.Errorf("UPDATE: displayName=%v, want 'Top Customers (Revised)'", updated["displayName"])
		}

		// --- GET reflects update ---
		rec = do(t, r, http.MethodGet, base+"/byRid/"+rid, nil)
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		if got["displayName"] != "Top Customers (Revised)" {
			t.Errorf("GET-after-update: displayName=%v, want revised", got["displayName"])
		}

		// --- DELETE ---
		rec = do(t, r, http.MethodDelete, base+"/byRid/"+rid, nil)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE status=%d, want 204; body=%s", rec.Code, rec.Body.String())
		}

		// --- GET after delete -> 404 ---
		rec = do(t, r, http.MethodGet, base+"/byRid/"+rid, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET-after-delete status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("create on unknown ontology returns 404", func(t *testing.T) {
		r, _ := newServer(t)
		rec := do(t, r, http.MethodPost,
			"/api/v2/ontologies/does-not-exist/queryTypes",
			map[string]any{"apiName": "x", "displayName": "X"})
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status=%d, want 404; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("create missing apiName returns 400", func(t *testing.T) {
		r, _ := newServer(t)
		rec := do(t, r, http.MethodPost,
			"/api/v2/ontologies/"+ontAPIName+"/queryTypes",
			map[string]any{"displayName": "No API name"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
	})
}
