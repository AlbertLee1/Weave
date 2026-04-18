package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newUserRoleHandlerHarness() (*UserRoleHandler, *fakeUserRepo, *fakeRoleRepo) {
	users := newFakeUserRepo()
	users.users["user:alice@example.com"] = &UserRecord{ID: "user:alice@example.com", Email: "alice@example.com"}
	roles := newFakeRoleRepo()
	h := NewUserRoleHandler(users, roles, users, nil)
	return h, users, roles
}

func TestUserRoleHandler_GrantRole_200(t *testing.T) {
	h, users, _ := newUserRoleHandlerHarness()

	body, _ := json.Marshal(map[string]any{"role": RoleEditor})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/roles", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantRoleFor(rec, req, "user:alice@example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	got := users.roles["user:alice@example.com"]
	if len(got) != 1 || got[0] != RoleEditor {
		t.Errorf("expected [editor], got %v", got)
	}
}

func TestUserRoleHandler_GrantRole_UnknownRole(t *testing.T) {
	h, _, _ := newUserRoleHandlerHarness()

	body, _ := json.Marshal(map[string]any{"role": "ghost"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/roles", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantRoleFor(rec, req, "user:alice@example.com")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserRoleHandler_GrantRole_UnknownUser(t *testing.T) {
	h, _, _ := newUserRoleHandlerHarness()

	body, _ := json.Marshal(map[string]any{"role": RoleViewer})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/user:ghost/roles", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.grantRoleFor(rec, req, "user:ghost")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestUserRoleHandler_ListRoles(t *testing.T) {
	h, users, _ := newUserRoleHandlerHarness()
	users.roles["user:alice@example.com"] = []string{RoleViewer, RoleEditor}

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/users/user:alice@example.com/roles", nil))
	rec := httptest.NewRecorder()
	h.listRolesFor(rec, req, "user:alice@example.com")
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp UserRolesResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Roles) != 2 {
		t.Errorf("expected 2 roles, got %v", resp.Roles)
	}
}

func TestUserRoleHandler_RevokeRole(t *testing.T) {
	h, users, _ := newUserRoleHandlerHarness()
	_ = users.UpsertUserRole(context.Background(), "user:alice@example.com", RoleEditor)
	_ = users.UpsertUserRole(context.Background(), "user:alice@example.com", RoleViewer)

	req := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/users/user:alice@example.com/roles/editor", nil))
	rec := httptest.NewRecorder()
	h.revokeRoleFor(rec, req, "user:alice@example.com", RoleEditor)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	got := users.roles["user:alice@example.com"]
	if len(got) != 1 || got[0] != RoleViewer {
		t.Errorf("expected [viewer], got %v", got)
	}
}

func TestUserRoleHandler_RevokeRole_Idempotent(t *testing.T) {
	h, _, _ := newUserRoleHandlerHarness()
	// Alice has no roles — revoking should still succeed (idempotent).
	req := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/users/user:alice@example.com/roles/editor", nil))
	rec := httptest.NewRecorder()
	h.revokeRoleFor(rec, req, "user:alice@example.com", RoleEditor)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204 (idempotent), got %d", rec.Code)
	}
}

func TestUserRoleHandler_GrantRole_RequiresAuth(t *testing.T) {
	h, _, _ := newUserRoleHandlerHarness()
	body, _ := json.Marshal(map[string]any{"role": RoleEditor})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/users/user:alice@example.com/roles", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.grantRoleFor(rec, req, "user:alice@example.com")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
