package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestMeHandler_DevAdmin(t *testing.T) {
	h := MeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	ctx := WithUser(req.Context(), &User{
		ID:    "dev-user",
		Roles: []string{RoleAdmin},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got struct {
		ID            string            `json:"id"`
		Email         string            `json:"email"`
		Name          string            `json:"name"`
		Roles         []string          `json:"roles"`
		OntologyRoles map[string]string `json:"ontologyRoles"`
		Permissions   []string          `json:"permissions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got.ID != "dev-user" {
		t.Errorf("expected id dev-user, got %q", got.ID)
	}
	if !slices.Contains(got.Roles, RoleAdmin) {
		t.Errorf("expected admin role, got %v", got.Roles)
	}
	if !slices.Contains(got.Permissions, PermSecurityPolicyManage) {
		t.Error("expected admin to have securityPolicy.manage permission")
	}
	if !slices.Contains(got.Permissions, PermOntologyRead) {
		t.Error("expected admin to have ontology.read permission")
	}
}

func TestMeHandler_Viewer(t *testing.T) {
	h := MeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	ctx := WithUser(req.Context(), &User{
		ID:    "alice",
		Roles: []string{RoleViewer},
	})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var got struct {
		Permissions []string `json:"permissions"`
	}
	json.NewDecoder(rec.Body).Decode(&got)
	if slices.Contains(got.Permissions, PermActionExecute) {
		t.Error("viewer must not have action.execute permission")
	}
	if !slices.Contains(got.Permissions, PermOntologyRead) {
		t.Error("viewer must have ontology.read permission")
	}
}

func TestMeHandler_Unauthenticated(t *testing.T) {
	h := MeHandler()
	req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
