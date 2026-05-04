package actiontemplates

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

// stubResolver is a TeammateResolver returning a fixed mate map for
// tests that exercise Scope=TEAM.
type stubResolver struct {
	mates map[string][]string
}

func (s stubResolver) Teammates(_ context.Context, callerID string) ([]string, error) {
	return s.mates[callerID], nil
}

func newTestRouter(store Store, user *auth.User) http.Handler {
	return newTestRouterWithResolver(store, user, nil)
}

func newTestRouterWithResolver(store Store, user *auth.User, resolver TeammateResolver) http.Handler {
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
	NewHandler(store).WithTeammateResolver(resolver).RegisterRoutes(r)
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

	createBody := mustEncode(t, map[string]any{
		"name":       "Daily Reorder",
		"ontology":   "main",
		"actionType": "createOrder",
		"parameters": map[string]any{"qty": 1, "sku": "WIDGET"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created Template
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "Daily Reorder" || created.CreatedBy != alice.ID || created.ID == "" {
		t.Fatalf("create returned wrong shape: %+v", created)
	}
	if created.Shared || created.Scope != ScopePrivate {
		t.Fatalf("default scope should be PRIVATE: %+v", created)
	}

	// Duplicate (owner, actionType, name) → 409
	req = httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(createBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate POST: want 409, got %d", w.Code)
	}

	// Empty name → 400
	bad := mustEncode(t, map[string]any{
		"name":       "",
		"ontology":   "main",
		"actionType": "createOrder",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(bad))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty name POST: want 400, got %d", w.Code)
	}

	// Empty actionType → 400
	bad = mustEncode(t, map[string]any{
		"name":       "x",
		"ontology":   "main",
		"actionType": "",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(bad))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty actionType POST: want 400, got %d", w.Code)
	}

	// Invalid scope → 400
	bad = mustEncode(t, map[string]any{
		"name":       "InvalidScopeRow",
		"ontology":   "main",
		"actionType": "createOrder",
		"scope":      "ORG",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(bad))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid scope POST: want 400, got %d", w.Code)
	}

	// LIST scoped — single result for the (main, createOrder) tuple
	req = httptest.NewRequest(http.MethodGet, "/api/v2/action-templates?ontology=main&actionType=createOrder", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET list: want 200, got %d", w.Code)
	}
	var listResp listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listResp.ActionTemplates) != 1 || listResp.ActionTemplates[0].ID != created.ID {
		t.Fatalf("list returned %+v", listResp.ActionTemplates)
	}

	// GET single
	req = httptest.NewRequest(http.MethodGet, "/api/v2/action-templates/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET single: want 200, got %d", w.Code)
	}

	// PUT rename + flip to PUBLIC via explicit scope
	publicScope := ScopePublic
	rename := mustEncode(t, map[string]any{"name": "Reorder", "scope": &publicScope})
	req = httptest.NewRequest(http.MethodPut, "/api/v2/action-templates/"+created.ID, bytes.NewReader(rename))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT rename: want 200, got %d (%s)", w.Code, w.Body.String())
	}
	var renamed Template
	if err := json.Unmarshal(w.Body.Bytes(), &renamed); err != nil {
		t.Fatalf("decode renamed: %v", err)
	}
	if renamed.Name != "Reorder" || renamed.Scope != ScopePublic || !renamed.Shared {
		t.Fatalf("rename+share did not persist: %+v", renamed)
	}

	// Now bob can see the row (it's PUBLIC) but cannot mutate it.
	bobR := newTestRouter(store, &auth.User{ID: "user:bob"})
	req = httptest.NewRequest(http.MethodGet, "/api/v2/action-templates/"+created.ID, nil)
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("public GET (non-owner): want 200, got %d", w.Code)
	}
	bobRename := mustEncode(t, map[string]any{"name": "Hijack"})
	req = httptest.NewRequest(http.MethodPut, "/api/v2/action-templates/"+created.ID, bytes.NewReader(bobRename))
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner PUT on public row: want 404, got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/action-templates/"+created.ID, nil)
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner DELETE on public row: want 404, got %d", w.Code)
	}

	// Cross-user access on a private row → 404 (id leak protection)
	private := mustEncode(t, map[string]any{
		"name":       "Private",
		"ontology":   "main",
		"actionType": "createOrder",
	})
	req = httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(private))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("private POST: want 201, got %d", w.Code)
	}
	var privateRow Template
	_ = json.Unmarshal(w.Body.Bytes(), &privateRow)
	req = httptest.NewRequest(http.MethodGet, "/api/v2/action-templates/"+privateRow.ID, nil)
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("cross-user private GET: want 404, got %d", w.Code)
	}

	// Missing auth → 401
	noAuthR := newTestRouter(store, nil)
	req = httptest.NewRequest(http.MethodGet, "/api/v2/action-templates", nil)
	w = httptest.NewRecorder()
	noAuthR.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous GET: want 401, got %d", w.Code)
	}

	// DELETE owner
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/action-templates/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: want 204, got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/action-templates/"+created.ID, nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DELETE missing: want 404, got %d", w.Code)
	}
}

func TestHandler_LegacySharedField(t *testing.T) {
	// US-320 SDK clients still POST `shared:true` without `scope` —
	// the handler must map that to PUBLIC.
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	r := newTestRouter(store, alice)

	body := mustEncode(t, map[string]any{
		"name":       "Legacy Shared",
		"ontology":   "main",
		"actionType": "createOrder",
		"shared":     true,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("legacy POST: want 201, got %d (%s)", w.Code, w.Body.String())
	}
	var created Template
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Scope != ScopePublic || !created.Shared {
		t.Fatalf("legacy shared=true should map to PUBLIC: %+v", created)
	}
}

func TestHandler_ListVisibilityIncludesShared(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}

	// alice creates a private template; bob creates a PUBLIC one.
	aR := newTestRouter(store, alice)
	bR := newTestRouter(store, bob)
	aliceBody := mustEncode(t, map[string]any{
		"name":       "Alice Private",
		"ontology":   "main",
		"actionType": "createOrder",
	})
	w := httptest.NewRecorder()
	aR.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(aliceBody)))
	if w.Code != http.StatusCreated {
		t.Fatalf("alice POST: %d", w.Code)
	}
	bobBody := mustEncode(t, map[string]any{
		"name":       "Bob Public",
		"ontology":   "main",
		"actionType": "createOrder",
		"scope":      "PUBLIC",
	})
	w = httptest.NewRecorder()
	bR.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(bobBody)))
	if w.Code != http.StatusCreated {
		t.Fatalf("bob POST: %d", w.Code)
	}

	// Alice sees both rows in the list (her own + bob's PUBLIC).
	w = httptest.NewRecorder()
	aR.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/action-templates?ontology=main&actionType=createOrder", nil))
	var lr listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &lr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(lr.ActionTemplates) != 2 {
		t.Fatalf("alice list: want 2 rows, got %d (%+v)", len(lr.ActionTemplates), lr.ActionTemplates)
	}

	// Bob sees only his PUBLIC row (alice's is private to her).
	w = httptest.NewRecorder()
	bR.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/action-templates?ontology=main&actionType=createOrder", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &lr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(lr.ActionTemplates) != 1 || lr.ActionTemplates[0].Name != "Bob Public" {
		t.Fatalf("bob list: want 1 (Bob Public), got %+v", lr.ActionTemplates)
	}
}

func TestHandler_TeamScopeUsesResolver(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}
	carol := &auth.User{ID: "user:carol"}

	resolver := stubResolver{mates: map[string][]string{
		"user:bob": {"user:alice"}, // bob shares a group with alice
	}}

	aR := newTestRouterWithResolver(store, alice, resolver)
	bR := newTestRouterWithResolver(store, bob, resolver)
	cR := newTestRouterWithResolver(store, carol, resolver)

	// Alice creates a TEAM-scoped row.
	body := mustEncode(t, map[string]any{
		"name":       "Team Run",
		"ontology":   "main",
		"actionType": "createOrder",
		"scope":      "TEAM",
	})
	w := httptest.NewRecorder()
	aR.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("alice POST: %d (%s)", w.Code, w.Body.String())
	}
	var row Template
	_ = json.Unmarshal(w.Body.Bytes(), &row)

	// Bob (teammate) sees the row.
	w = httptest.NewRecorder()
	bR.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/action-templates/"+row.ID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("teammate GET: want 200, got %d", w.Code)
	}

	// Carol (no shared group) does not.
	w = httptest.NewRecorder()
	cR.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/action-templates/"+row.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-teammate GET: want 404, got %d", w.Code)
	}

	// Carol cannot mutate.
	w = httptest.NewRecorder()
	cR.ServeHTTP(w, httptest.NewRequest(http.MethodDelete, "/api/v2/action-templates/"+row.ID, nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-teammate DELETE: want 404, got %d", w.Code)
	}
}

func TestHandler_DegradedModeNoStore(t *testing.T) {
	r := newTestRouter(nil, &auth.User{ID: "user:alice"})
	req := httptest.NewRequest(http.MethodGet, "/api/v2/action-templates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("nil store list: want 500 ActionTemplatesUnavailable, got %d", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["errorName"] != "ActionTemplatesUnavailable" {
		t.Fatalf("unexpected errorName: %v", resp["errorName"])
	}
}

func TestNewTemplateID_RFC4122Shape(t *testing.T) {
	id := newTemplateID()
	if len(id) != 36 {
		t.Fatalf("id length = %d, want 36", len(id))
	}
	for _, idx := range []int{8, 13, 18, 23} {
		if id[idx] != '-' {
			t.Fatalf("id missing dash at idx %d: %q", idx, id)
		}
	}
	if id[14] != '4' {
		t.Fatalf("id version nibble: want '4', got %c", id[14])
	}
}
