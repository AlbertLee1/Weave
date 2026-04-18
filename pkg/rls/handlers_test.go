package rls

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/auth"
)

func mustMarshal(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func withAdminUser(r *http.Request) *http.Request {
	u := &auth.User{ID: "user:admin@ex.com", Email: "admin@ex.com", Roles: []string{auth.RoleAdmin}}
	return r.WithContext(auth.WithUser(r.Context(), u))
}

func mountHandler(t *testing.T, store Store) (*chi.Mux, *Handler) {
	t.Helper()
	handler := NewHandler(store, nil, nil)
	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	return r, handler
}

func decodeBody(t *testing.T, body io.Reader, into interface{}) {
	t.Helper()
	if err := json.NewDecoder(body).Decode(into); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestHandler_Create(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)

	body := mustMarshal(t, CreateRequest{
		ObjectTypeRID: "ri.ontology.main.object-type.Customer",
		Predicate:     json.RawMessage(`{"type":"eq","field":"region","value":"EU"}`),
		AppliesTo:     AppliesTo{Roles: []string{"eu-reader"}},
		Description:   "EU rows only",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/row-policies", bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp RowPolicy
	decodeBody(t, w.Body, &resp)
	if resp.RID == "" {
		t.Fatalf("expected RID to be populated")
	}
	if !strings.HasPrefix(resp.RID, "ri.rls.main.row-policy.") {
		t.Fatalf("unexpected RID prefix: %s", resp.RID)
	}

	got, err := store.Get(context.Background(), resp.RID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got.Description != "EU rows only" {
		t.Fatalf("description not persisted, got %q", got.Description)
	}
}

func TestHandler_Create_MissingUser_401(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)
	body := mustMarshal(t, CreateRequest{
		ObjectTypeRID: "ri.ontology.main.object-type.Customer",
		Predicate:     json.RawMessage(`{"type":"eq","field":"x","value":"y"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/row-policies", bytes.NewReader(body))
	// Note: no auth context.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestHandler_Create_Validation(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)
	body := mustMarshal(t, CreateRequest{
		// missing ObjectTypeRID
		Predicate: json.RawMessage(`{"type":"eq","field":"x","value":"y"}`),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/row-policies", bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_List(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.one",
		ObjectTypeRID: "ri.ontology.main.object-type.Customer",
		Predicate:     json.RawMessage(`{"type":"eq","field":"region","value":"EU"}`),
		AppliesTo:     AppliesTo{Roles: []string{"r"}},
	})
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.two",
		ObjectTypeRID: "ri.ontology.main.object-type.Order",
		Predicate:     json.RawMessage(`{"type":"eq","field":"owner","value":"alice"}`),
		AppliesTo:     AppliesTo{Users: []string{"alice"}},
	})

	router, _ := mountHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/row-policies", nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp ListResponse
	decodeBody(t, w.Body, &resp)
	if len(resp.Policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(resp.Policies))
	}
}

func TestHandler_List_FilterByObjectType(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.one",
		ObjectTypeRID: "ri.ontology.main.object-type.Customer",
		Predicate:     json.RawMessage(`{"type":"eq","field":"region","value":"EU"}`),
		AppliesTo:     AppliesTo{Roles: []string{"r"}},
	})
	_ = store.Create(ctx, &RowPolicy{
		RID:           "ri.rls.main.row-policy.two",
		ObjectTypeRID: "ri.ontology.main.object-type.Order",
		Predicate:     json.RawMessage(`{"type":"eq","field":"owner","value":"alice"}`),
		AppliesTo:     AppliesTo{Users: []string{"alice"}},
	})

	router, _ := mountHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/row-policies?objectType=ri.ontology.main.object-type.Customer", nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp ListResponse
	decodeBody(t, w.Body, &resp)
	if len(resp.Policies) != 1 {
		t.Fatalf("expected 1 policy filtered by objectType, got %d", len(resp.Policies))
	}
	if resp.Policies[0].RID != "ri.rls.main.row-policy.one" {
		t.Fatalf("expected filtered RID 'one', got %q", resp.Policies[0].RID)
	}
}

func TestHandler_GetUpdateDelete(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	rid := "ri.rls.main.row-policy.eu"
	_ = store.Create(ctx, &RowPolicy{
		RID:           rid,
		ObjectTypeRID: "ri.ontology.main.object-type.Customer",
		Predicate:     json.RawMessage(`{"type":"eq","field":"region","value":"EU"}`),
		AppliesTo:     AppliesTo{Roles: []string{"eu-reader"}},
		Description:   "EU",
	})

	router, _ := mountHandler(t, store)

	// Get
	req := httptest.NewRequest(http.MethodGet, "/api/admin/row-policies/"+rid, nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Get: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Update
	newDesc := "EU rows via new description"
	upd := mustMarshal(t, RowPolicyUpdate{Description: &newDesc})
	req = httptest.NewRequest(http.MethodPatch, "/api/admin/row-policies/"+rid, bytes.NewReader(upd))
	req = withAdminUser(req)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Update: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var got RowPolicy
	decodeBody(t, w.Body, &got)
	if got.Description != newDesc {
		t.Fatalf("Update did not persist description: got %q", got.Description)
	}

	// Delete
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/row-policies/"+rid, nil)
	req = withAdminUser(req)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Delete: expected 204, got %d", w.Code)
	}

	// Get again → 404
	req = httptest.NewRequest(http.MethodGet, "/api/admin/row-policies/"+rid, nil)
	req = withAdminUser(req)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("Get after delete: expected 404, got %d", w.Code)
	}
}

func TestHandler_Get_Unknown_404(t *testing.T) {
	store := NewMemoryStore()
	router, _ := mountHandler(t, store)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/row-policies/ri.rls.main.row-policy.ghost", nil)
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestHandler_WriteRefreshesEngine(t *testing.T) {
	store := NewMemoryStore()
	engine := New(store, nil)
	if err := engine.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	handler := NewHandler(store, nil, engine)
	r := chi.NewRouter()
	handler.RegisterRoutes(r)

	body := mustMarshal(t, CreateRequest{
		ObjectTypeRID: "ri.ontology.main.object-type.Customer",
		Predicate:     json.RawMessage(`{"type":"eq","field":"region","value":"EU"}`),
		AppliesTo:     AppliesTo{Roles: []string{"eu-reader"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/row-policies", bytes.NewReader(body))
	req = withAdminUser(req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if engine.Size("ri.ontology.main.object-type.Customer") != 1 {
		t.Fatalf("expected engine to see 1 policy for ObjectType after create, got %d",
			engine.Size("ri.ontology.main.object-type.Customer"))
	}
}
