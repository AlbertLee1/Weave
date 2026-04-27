package featureflags

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func newTestRouter(store Store) http.Handler {
	r := chi.NewRouter()
	caller := &auth.User{ID: "admin-user", Roles: []string{"admin"}}
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.WithUser(req.Context(), caller)))
		})
	})
	h := NewHandler(store)
	h.RegisterRoutes(r)
	return r
}

func TestHandler_CreateGetListUpdateDelete(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store)

	// CREATE
	body, _ := json.Marshal(map[string]any{
		"name":        "new-ui",
		"description": "try out the new UI",
		"enabled":     true,
		"realms":      []string{"main"},
		"users":       []string{"u1", "u2"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/feature-flags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created Flag
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "new-ui" || !created.Enabled {
		t.Fatalf("created wrong shape: %+v", created)
	}

	// Duplicate CREATE → 409.
	req = httptest.NewRequest(http.MethodPost, "/api/admin/feature-flags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate POST: want 409, got %d", w.Code)
	}

	// GET single.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/feature-flags/new-ui", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET single: want 200, got %d", w.Code)
	}

	// GET missing → 404.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/feature-flags/missing", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET missing: want 404, got %d", w.Code)
	}

	// LIST.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/feature-flags", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET list: want 200, got %d", w.Code)
	}
	var listResp struct {
		Flags []Flag `json:"flags"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.Flags) != 1 || listResp.Flags[0].Name != "new-ui" {
		t.Fatalf("list: %+v", listResp)
	}

	// UPDATE (enable=false, users cleared).
	updBody, _ := json.Marshal(map[string]any{
		"enabled": false,
		"users":   []string{},
	})
	req = httptest.NewRequest(http.MethodPut, "/api/admin/feature-flags/new-ui", bytes.NewReader(updBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var updated Flag
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode put: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("PUT did not disable flag: %+v", updated)
	}
	if len(updated.Users) != 0 {
		t.Fatalf("PUT did not clear users: %+v", updated)
	}

	// DELETE.
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/feature-flags/new-ui", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d", w.Code)
	}

	// DELETE missing → 404.
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/feature-flags/new-ui", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing: want 404, got %d", w.Code)
	}
}

func TestHandler_CreateValidation(t *testing.T) {
	store := NewMemoryStore()
	r := newTestRouter(store)

	cases := []struct {
		name       string
		body       map[string]any
		wantStatus int
	}{
		{"empty name", map[string]any{"name": ""}, http.StatusBadRequest},
		{"whitespace name", map[string]any{"name": "has space"}, http.StatusBadRequest},
		{"valid minimal", map[string]any{"name": "flag-ok"}, http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPost, "/api/admin/feature-flags", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("%s: want %d, got %d (%s)", tc.name, tc.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestHandler_UnauthorizedWhenNoUser(t *testing.T) {
	store := NewMemoryStore()
	h := NewHandler(store)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/feature-flags", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unauth: want 401, got %d", w.Code)
	}
}
