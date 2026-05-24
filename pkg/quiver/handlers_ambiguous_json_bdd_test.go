package quiver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_Quiver_Save_RejectsAmbiguousJSONBody closes the same
// ambiguous-JSON smuggling class P2A-301..306 closed for pkg/auth
// admin write surfaces. pkg/quiver.Save used
// `json.NewDecoder(r.Body).Decode(&req)` which accepts only the
// first JSON value and silently drops trailing bytes. A body
// composed of two concatenated objects decodes cleanly to the
// first while a proxy / WAF / log scraper re-reading the raw bytes
// can be tricked into believing different config landed.
//
// Fix routes the handler through pkg/httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF, returning a 400 with a
// "single JSON value" reason. BDD covers the rejection + a
// well-formed regression so the SDK happy path stays green.
func TestBDD_Quiver_Save_RejectsAmbiguousJSONBody(t *testing.T) {
	alice := &auth.User{ID: "user:alice"}

	t.Run("Save rejects concatenated JSON without persisting any dashboard", func(t *testing.T) {
		store := NewMemoryStore()
		r := newTestRouter(store, alice)

		// {"name":"Quiver-A"}{"name":"Quiver-B","config":{"public":true}}
		body := `{"name":"Quiver-A"}{"name":"Quiver-B","config":{"public":true}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v2/quiver/save", bytes.NewReader([]byte(body)))
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
		// Non-mutation snapshot: no dashboard was saved.
		got, _ := store.List(req.Context(), alice.ID)
		if len(got) != 0 {
			t.Errorf("ambiguous body must not persist any dashboard; got %d", len(got))
		}
	})

	t.Run("Save with well-formed body still succeeds (regression guard)", func(t *testing.T) {
		store := NewMemoryStore()
		r := newTestRouter(store, alice)
		body := mustEncode(t, map[string]any{"name": "Quiver-One", "config": map[string]any{"a": 1}})
		req := httptest.NewRequest(http.MethodPost, "/api/v2/quiver/save", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK && w.Code != http.StatusCreated {
			t.Fatalf("status=%d, want 200/201; body=%s", w.Code, w.Body.String())
		}
	})
}
