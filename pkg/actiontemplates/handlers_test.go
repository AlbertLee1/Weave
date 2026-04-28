package actiontemplates

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
	if created.Shared {
		t.Fatalf("default shared should be false: %+v", created)
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

	// PUT rename + share
	shareTrue := true
	rename := mustEncode(t, map[string]any{"name": "Reorder", "shared": &shareTrue})
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
	if renamed.Name != "Reorder" || !renamed.Shared {
		t.Fatalf("rename+share did not persist: %+v", renamed)
	}

	// Now bob can see the row (it's shared) but cannot mutate it.
	bobR := newTestRouter(store, &auth.User{ID: "user:bob"})
	req = httptest.NewRequest(http.MethodGet, "/api/v2/action-templates/"+created.ID, nil)
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("shared GET (non-owner): want 200, got %d", w.Code)
	}
	bobRename := mustEncode(t, map[string]any{"name": "Hijack"})
	req = httptest.NewRequest(http.MethodPut, "/api/v2/action-templates/"+created.ID, bytes.NewReader(bobRename))
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner PUT on shared row: want 404, got %d", w.Code)
	}
	req = httptest.NewRequest(http.MethodDelete, "/api/v2/action-templates/"+created.ID, nil)
	w = httptest.NewRecorder()
	bobR.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("non-owner DELETE on shared row: want 404, got %d", w.Code)
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

func TestHandler_ListVisibilityIncludesShared(t *testing.T) {
	store := NewMemoryStore()
	alice := &auth.User{ID: "user:alice"}
	bob := &auth.User{ID: "user:bob"}

	// alice creates a private template; bob creates a shared one.
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
		"name":       "Bob Shared",
		"ontology":   "main",
		"actionType": "createOrder",
		"shared":     true,
	})
	w = httptest.NewRecorder()
	bR.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v2/action-templates", bytes.NewReader(bobBody)))
	if w.Code != http.StatusCreated {
		t.Fatalf("bob POST: %d", w.Code)
	}

	// Alice sees both rows in the list (her own + bob's shared).
	w = httptest.NewRecorder()
	aR.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/action-templates?ontology=main&actionType=createOrder", nil))
	var lr listResponse
	if err := json.Unmarshal(w.Body.Bytes(), &lr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(lr.ActionTemplates) != 2 {
		t.Fatalf("alice list: want 2 rows, got %d (%+v)", len(lr.ActionTemplates), lr.ActionTemplates)
	}

	// Bob sees only his shared row (alice's is private to her).
	w = httptest.NewRecorder()
	bR.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v2/action-templates?ontology=main&actionType=createOrder", nil))
	if err := json.Unmarshal(w.Body.Bytes(), &lr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(lr.ActionTemplates) != 1 || lr.ActionTemplates[0].Name != "Bob Shared" {
		t.Fatalf("bob list: want 1 (Bob Shared), got %+v", lr.ActionTemplates)
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
