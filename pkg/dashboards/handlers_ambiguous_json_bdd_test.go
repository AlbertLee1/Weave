package dashboards

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_Dashboards_RejectAmbiguousJSONBody closes the same
// ambiguous-JSON smuggling class P2A-301..306 closed for pkg/auth
// admin write surfaces. pkg/dashboards.Create + Update both used
// `json.NewDecoder(r.Body).Decode(&req)` which accepts only the
// first JSON value and silently drops trailing bytes. A body
// composed of two concatenated objects decodes cleanly to the
// first while a proxy / WAF / log scraper re-reading the raw bytes
// can be tricked into believing different config landed.
//
// Fix routes both handlers through pkg/httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF, returning a 400 with a
// "single JSON value" reason. BDD covers both endpoints and an
// empty-body / well-formed regression so the fix doesn't break the
// SDK happy path.
func TestBDD_Dashboards_RejectAmbiguousJSONBody(t *testing.T) {
	alice := &auth.User{ID: "user:alice"}

	t.Run("Create rejects concatenated JSON without persisting any dashboard", func(t *testing.T) {
		store := NewMemoryStore()
		r := newTestRouter(store, alice)

		// {"name":"Public"}{"name":"Smuggled","isPublic":true}
		// — the first decoded value would land as a private "Public"
		// dashboard; the smuggled trailer could be observed by an
		// audit pipeline re-parsing the raw bytes as if the operator
		// authorised creating a second public "Smuggled" dashboard.
		body := `{"name":"Public"}{"name":"Smuggled","isPublic":true}`
		req := httptest.NewRequest(http.MethodPost, "/api/v2/dashboards", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
		}
		var env struct {
			ErrorName  string            `json:"errorName"`
			Parameters map[string]string `json:"parameters"`
		}
		_ = json.NewDecoder(w.Body).Decode(&env)
		if env.ErrorName != "InvalidRequestBody" {
			t.Errorf("errorName: got %q, want InvalidRequestBody", env.ErrorName)
		}
		if !strings.Contains(strings.ToLower(env.Parameters["reason"]), "single json value") {
			t.Errorf("reason should mention single JSON value, got %q", env.Parameters["reason"])
		}
		// Non-mutation snapshot: no dashboard was persisted.
		got, _ := store.List(req.Context(), alice.ID)
		if len(got) != 0 {
			t.Errorf("ambiguous body must not persist any dashboard; got %d", len(got))
		}
	})

	t.Run("Create with well-formed body still succeeds (regression guard)", func(t *testing.T) {
		store := NewMemoryStore()
		r := newTestRouter(store, alice)
		body := mustEncode(t, map[string]any{"name": "Sales"})
		req := httptest.NewRequest(http.MethodPost, "/api/v2/dashboards", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("status=%d, want 201; body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("Update rejects concatenated JSON without persisting the change", func(t *testing.T) {
		store := NewMemoryStore()
		r := newTestRouter(store, alice)

		// Seed a dashboard via the happy create path so we have an id to update.
		create := mustEncode(t, map[string]any{"name": "Original"})
		createReq := httptest.NewRequest(http.MethodPost, "/api/v2/dashboards", bytes.NewReader(create))
		createReq.Header.Set("Content-Type", "application/json")
		createRec := httptest.NewRecorder()
		r.ServeHTTP(createRec, createReq)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("seed dashboard: status=%d body=%s", createRec.Code, createRec.Body.String())
		}
		var seeded Dashboard
		_ = json.Unmarshal(createRec.Body.Bytes(), &seeded)

		// Now PUT with concatenated body — first decode would rename to
		// "Public-Rename"; trailing would also try to flip isPublic.
		body := `{"name":"Public-Rename"}{"isPublic":true}`
		req := httptest.NewRequest(http.MethodPut, "/api/v2/dashboards/"+seeded.ID, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
		}
		// Non-mutation snapshot: row name stayed "Original" and isPublic stayed false.
		got, err := store.Get(req.Context(), seeded.ID, alice.ID)
		if err != nil {
			t.Fatalf("Get after rejected PUT: %v", err)
		}
		if got.Name != "Original" {
			t.Errorf("ambiguous PUT mutated name: got %q want Original", got.Name)
		}
		if got.IsPublic {
			t.Errorf("ambiguous PUT smuggled isPublic=true")
		}
	})
}
