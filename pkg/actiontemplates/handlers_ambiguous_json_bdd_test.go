package actiontemplates

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/auth"
)

// TestBDD_ActionTemplates_RejectsAmbiguousJSONBody continues the
// P2A-30x ambiguous-JSON hardening series (rounds 1, 15-19) into
// pkg/actiontemplates — the Foundry-adjacent Action Template
// preset surface. Two POST endpoints still decoded via
// `json.NewDecoder(r.Body).Decode(&req)`:
//
//   - POST /api/v2/action-templates          (Create)
//   - PUT  /api/v2/action-templates/{id}     (Update)
//
// Smuggling vector: an attacker submits
// `{"name":"Daily Reorder","scope":"PRIVATE"}{"scope":"SHARED"}`.
// The handler creates a private template while audit pipelines
// re-parsing the raw bytes see the trailing `scope=SHARED` as if
// the operator authorized a shared template — meaningful because
// SHARED templates are visible to teammates by design.
//
// Fix mirrors rounds 15-19: swap to httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF and returns 400 with the
// "single JSON value" reason.
func TestBDD_ActionTemplates_RejectsAmbiguousJSONBody(t *testing.T) {
	alice := &auth.User{ID: "user:alice"}

	t.Run("Create rejects concatenated JSON without persisting any template", func(t *testing.T) {
		store := NewMemoryStore()
		r := newTestRouter(store, alice)

		// {"name":"safe",...,"scope":"PRIVATE"}{"name":"smuggled","scope":"SHARED"}
		body := `{"name":"safe","ontology":"main","actionType":"createOrder","scope":"PRIVATE"}{"name":"smuggled","ontology":"main","actionType":"createOrder","scope":"SHARED"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertActionTemplateSingleJSONRejection(t, w)

		// Non-mutation snapshot: no template should have been persisted.
		got, _ := store.List(context.Background(), Visibility{CallerID: alice.ID}, "", "")
		if len(got) != 0 {
			t.Errorf("ambiguous body must not persist any template; got %d", len(got))
		}
	})

	t.Run("Create with well-formed body still succeeds (regression guard)", func(t *testing.T) {
		store := NewMemoryStore()
		r := newTestRouter(store, alice)
		body := mustEncode(t, map[string]any{
			"name":       "Happy",
			"ontology":   "main",
			"actionType": "createOrder",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("happy Create: status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("Update rejects concatenated JSON without persisting the change", func(t *testing.T) {
		store := NewMemoryStore()
		r := newTestRouter(store, alice)

		// Seed a template via the happy create path.
		seed := mustEncode(t, map[string]any{
			"name":       "Original",
			"ontology":   "main",
			"actionType": "createOrder",
		})
		createReq := httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(seed))
		createReq.Header.Set("Content-Type", "application/json")
		createRec := httptest.NewRecorder()
		r.ServeHTTP(createRec, createReq)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("seed: status=%d body=%s", createRec.Code, createRec.Body.String())
		}
		var seeded Template
		_ = json.Unmarshal(createRec.Body.Bytes(), &seeded)

		// PUT with concatenated body — first decode renames to
		// "Renamed-Safe"; trailing would smuggle a scope flip.
		body := `{"name":"Renamed-Safe"}{"scope":"SHARED"}`
		req := httptest.NewRequest(http.MethodPut, "/api/v2/action-templates/"+seeded.ID, bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assertActionTemplateSingleJSONRejection(t, w)

		// Non-mutation snapshot: name stays "Original" and scope stays PRIVATE.
		got, err := store.Get(context.Background(), seeded.ID, Visibility{CallerID: alice.ID})
		if err != nil {
			t.Fatalf("Get after rejected PUT: %v", err)
		}
		if got.Name != "Original" {
			t.Errorf("ambiguous PUT mutated name: got %q want Original", got.Name)
		}
		if got.Scope != ScopePrivate {
			t.Errorf("ambiguous PUT smuggled scope: got %q want %q", got.Scope, ScopePrivate)
		}
	})
}

func assertActionTemplateSingleJSONRejection(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorName != "InvalidRequestBody" {
		t.Errorf("errorName: got %q, want InvalidRequestBody", env.ErrorName)
	}
	if !strings.Contains(strings.ToLower(env.Parameters["reason"]), "single json value") {
		t.Errorf("reason should mention 'single JSON value', got %q", env.Parameters["reason"])
	}
}
