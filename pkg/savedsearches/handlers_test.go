package savedsearches

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func newTestRouter(store Store, user *auth.User) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			if user != nil {
				ctx = auth.WithUser(ctx, user)
			}
			next.ServeHTTP(w, req.WithContext(ctx))
		})
	})
	NewHandler(store).RegisterRoutes(r)
	return r
}

func mustEncode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestHandler_CreateListGetUpdateDelete(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	r := newTestRouter(store, alice)

	// CREATE
	createBody := mustEncode(t, map[string]any{
		"name":       "Recent Apples",
		"ontology":   "main",
		"objectType": "produce",
		"definition": map[string]any{
			"searchText": "apples",
			"facets":     map[string][]string{"category": {"fruit"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/saved-searches", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created SavedSearch
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "Recent Apples" || created.CreatedBy != alice.ID || created.ID == "" {
		t.Fatalf("create returned wrong shape: %+v", created)
	}

	// CREATE duplicate name → 409
	req = httptest.NewRequest(http.MethodPost, "/api/v2/saved-searches", bytes.NewReader(createBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate POST: want 409, got %d", w.Code)
	}

	// CREATE with empty name → 400
	bad := mustEncode(t, map[string]any{
		"name":       "",
		"ontology":   "main",
		"objectType": "produce",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/saved-searches", bytes.NewReader(bad))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty name POST: want 400, got %d", w.Code)
	}

	// LIST scoped — single result for the (main, produce) tuple
	req = httptest.NewRequest(http.MethodGet, "/api/v2/saved-searches?ontology=main&objectType=produce", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET list: want 200, got %d", w.Code)
	}
	var listResp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.SavedSearches) != 1 || listResp.SavedSearches[0].ID != created.ID {
		t.Fatalf("list returned %+v", listResp.SavedSearches)
	}

	// LIST scoped to a different objectType — empty
	req = httptest.NewRequest(http.MethodGet, "/api/v2/saved-searches?ontology=main&objectType=other", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET other-list: want 200, got %d", w.Code)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode other-list: %v", err)
	}
	if len(listResp.SavedSearches) != 0 {
		t.Fatalf("other-tab list should be empty, got %+v", listResp.SavedSearches)
	}

	// GET single
	req = httptest.NewRequest(http.MethodGet, "/api/v2/saved-searches/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET single: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	// PUT rename
	rename := mustEncode(t, map[string]any{"name": "Apples"})
	req = httptest.NewRequest(http.MethodPut, "/api/v2/saved-searches/"+created.ID, bytes.NewReader(rename))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT rename: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	// Cross-user access → 404 (not 403, to avoid leaking ids)
	bobR := newTestRouter(store, &auth.User{ID: "user:bob"})
	req = httptest.NewRequest(http.MethodGet, "/api/v2/saved-searches/"+created.ID, nil)
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-user GET: want 404, got %d", w.Code)
	}

	// Missing auth → 401
	noAuthR := newTestRouter(store, nil)
	req = httptest.NewRequest(http.MethodGet, "/api/v2/saved-searches", nil)
	w = httptest.NewRecorder()
	noAuthR.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET: want 401, got %d", w.Code)
	}

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/saved-searches/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d", w.Code)
	}
	// DELETE missing → 404
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/saved-searches/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing: want 404, got %d", w.Code)
	}
}

func TestHandler_DegradedModeNoStore(t *testing.T) {
	r := newTestRouter(nil, &auth.User{ID: "user:alice"})
	req := httptest.NewRequest(http.MethodGet, "/api/v2/saved-searches", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil store list: want 500 SavedSearchesUnavailable, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["errorName"] != "SavedSearchesUnavailable" {
		t.Fatalf("unexpected errorName: %v", resp["errorName"])
	}
}
