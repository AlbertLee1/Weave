package dashboards

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
		"name":       "Sales",
		"definition": map[string]any{"widgets": []any{}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/dashboards", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created Dashboard
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "Sales" || created.CreatedBy != alice.ID || created.ID == "" {
		t.Fatalf("create returned wrong shape: %+v", created)
	}

	// CREATE duplicate name → 409
	req = httptest.NewRequest(http.MethodPost, "/api/v2/dashboards", bytes.NewReader(createBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate POST: want 409, got %d", w.Code)
	}

	// CREATE with empty name → 400
	bad := mustEncode(t, map[string]any{"name": ""})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/dashboards", bytes.NewReader(bad))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty name POST: want 400, got %d", w.Code)
	}

	// LIST
	req = httptest.NewRequest(http.MethodGet, "/api/v2/dashboards", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET list: want 200, got %d", w.Code)
	}
	var listResp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Dashboards) != 1 || listResp.Dashboards[0].ID != created.ID {
		t.Fatalf("list returned %+v", listResp.Dashboards)
	}

	// GET single
	req = httptest.NewRequest(http.MethodGet, "/api/v2/dashboards/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET single: want 200, got %d (%s)", w.Code, w.Body.String())
	}

	// PUT rename + publish
	rename := mustEncode(t, map[string]any{"name": "Sales-2", "isPublic": true})
	req = httptest.NewRequest(http.MethodPut, "/api/v2/dashboards/"+created.ID, bytes.NewReader(rename))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT rename: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var renamed Dashboard
	if err := json.Unmarshal(w.Body.Bytes(), &renamed); err != nil {
		t.Fatalf("decode rename: %v", err)
	}
	if renamed.Name != "Sales-2" || !renamed.IsPublic {
		t.Fatalf("rename/publish did not persist: %+v", renamed)
	}

	// Cross-user GET on a private dashboard → 404
	priv := mustEncode(t, map[string]any{
		"name":       "Private",
		"definition": map[string]any{"widgets": []any{}},
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/dashboards", bytes.NewReader(priv))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create private: want 201, got %d", w.Code)
	}
	var privCreated Dashboard
	_ = json.Unmarshal(w.Body.Bytes(), &privCreated)

	bobR := newTestRouter(store, &auth.User{ID: "user:bob"})
	req = httptest.NewRequest(http.MethodGet, "/api/v2/dashboards/"+privCreated.ID, nil)
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-user GET private: want 404, got %d", w.Code)
	}

	// Public dashboards are readable by other authenticated users.
	req = httptest.NewRequest(http.MethodGet, "/api/v2/dashboards/"+created.ID, nil)
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cross-user GET public: want 200, got %d", w.Code)
	}

	// Cross-user PUT remains forbidden as 404.
	req = httptest.NewRequest(http.MethodPut, "/api/v2/dashboards/"+created.ID, bytes.NewReader(rename))
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-user PUT: want 404, got %d", w.Code)
	}

	// Missing auth → 401
	noAuthR := newTestRouter(store, nil)
	req = httptest.NewRequest(http.MethodGet, "/api/v2/dashboards", nil)
	w = httptest.NewRecorder()
	noAuthR.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET: want 401, got %d", w.Code)
	}

	// DELETE
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/dashboards/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d", w.Code)
	}
	// DELETE missing → 404
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/dashboards/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing: want 404, got %d", w.Code)
	}
}

func TestHandler_DegradedModeNoStore(t *testing.T) {
	r := newTestRouter(nil, &auth.User{ID: "user:alice"})
	req := httptest.NewRequest(http.MethodGet, "/api/v2/dashboards", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil store list: want 500 DashboardsUnavailable, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["errorName"] != "DashboardsUnavailable" {
		t.Fatalf("unexpected errorName: %v", resp["errorName"])
	}
}
