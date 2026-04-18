package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"
)

// fakeRoleRepo is an in-memory RoleRepository for handler tests.
type fakeRoleRepo struct {
	mu          sync.Mutex
	roles       map[string]*Role
	permissions map[string][]string
}

func newFakeRoleRepo() *fakeRoleRepo {
	r := &fakeRoleRepo{roles: map[string]*Role{}, permissions: map[string][]string{}}
	// Seed built-ins to mirror migration 000051.
	for _, name := range []string{RoleViewer, RoleEditor, RoleOntologyOwner, RoleAdmin, RoleIngestWriter} {
		r.roles[name] = &Role{Name: name, Builtin: true, CreatedAt: time.Now()}
	}
	return r
}

func (f *fakeRoleRepo) Create(_ context.Context, role *Role) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.roles[role.Name]; ok {
		return ErrRoleConflict
	}
	role.CreatedAt = time.Now()
	cp := *role
	f.roles[role.Name] = &cp
	return nil
}

func (f *fakeRoleRepo) Get(_ context.Context, name string) (*Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.roles[name]
	if !ok {
		return nil, ErrRoleNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *fakeRoleRepo) List(_ context.Context) ([]*Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*Role, 0, len(f.roles))
	for _, r := range f.roles {
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Builtin != out[j].Builtin {
			return out[i].Builtin
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

func (f *fakeRoleRepo) Delete(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.roles[name]; !ok {
		return ErrRoleNotFound
	}
	delete(f.roles, name)
	delete(f.permissions, name)
	return nil
}

func (f *fakeRoleRepo) UpdateDescription(_ context.Context, name, description string) (*Role, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.roles[name]
	if !ok {
		return nil, ErrRoleNotFound
	}
	r.Description = description
	cp := *r
	return &cp, nil
}

func (f *fakeRoleRepo) SetPermissions(_ context.Context, role string, perms []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.roles[role]; !ok {
		return ErrRoleNotFound
	}
	cp := make([]string, len(perms))
	copy(cp, perms)
	f.permissions[role] = cp
	return nil
}

func (f *fakeRoleRepo) ListPermissions(_ context.Context, role string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.roles[role]; !ok {
		return nil, ErrRoleNotFound
	}
	cp := make([]string, len(f.permissions[role]))
	copy(cp, f.permissions[role])
	return cp, nil
}

var _ RoleRepository = (*fakeRoleRepo)(nil)

func newRoleHandlerHarness() (*RoleHandler, *fakeRoleRepo) {
	repo := newFakeRoleRepo()
	return NewRoleHandler(repo, nil), repo
}

func TestRoleHandler_Create_201(t *testing.T) {
	h, _ := newRoleHandlerHarness()
	body, _ := json.Marshal(map[string]any{
		"name":        "data-scientist",
		"description": "ML team",
		"permissions": []string{PermObjectRead, PermActionExecute},
	})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/roles", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp RoleResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Name != "data-scientist" || resp.Builtin {
		t.Errorf("unexpected resp: %+v", resp)
	}
	if len(resp.Permissions) != 2 {
		t.Errorf("expected 2 perms, got %v", resp.Permissions)
	}
}

func TestRoleHandler_Create_RejectsInvalidName(t *testing.T) {
	h, _ := newRoleHandlerHarness()
	body, _ := json.Marshal(map[string]any{"name": "bad name"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/roles", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestRoleHandler_Create_Conflict(t *testing.T) {
	h, _ := newRoleHandlerHarness()
	body, _ := json.Marshal(map[string]any{"name": RoleAdmin})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/roles", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

func TestRoleHandler_List_BuiltinPermissionsFallback(t *testing.T) {
	h, _ := newRoleHandlerHarness()
	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/roles", nil))
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp RoleListResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)

	foundViewer := false
	for _, role := range resp.Roles {
		if role.Name == RoleViewer {
			foundViewer = true
			if !role.Builtin {
				t.Error("viewer should be builtin")
			}
			if len(role.Permissions) == 0 {
				t.Error("expected viewer to fall back to static permission matrix")
			}
		}
	}
	if !foundViewer {
		t.Error("viewer not in list")
	}
}

func TestRoleHandler_Delete_BuiltinProtected(t *testing.T) {
	h, _ := newRoleHandlerHarness()
	req := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/roles/admin", nil))
	rec := httptest.NewRecorder()
	h.deleteFor(rec, req, RoleAdmin)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRoleHandler_Delete_Custom(t *testing.T) {
	h, repo := newRoleHandlerHarness()
	_ = repo.Create(context.Background(), &Role{Name: "temp"})

	req := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/roles/temp", nil))
	rec := httptest.NewRecorder()
	h.deleteFor(rec, req, "temp")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if _, ok := repo.roles["temp"]; ok {
		t.Error("role still in repo")
	}
}

func TestRoleHandler_SetPermissions_CustomRole(t *testing.T) {
	h, repo := newRoleHandlerHarness()
	_ = repo.Create(context.Background(), &Role{Name: "data-scientist"})

	body, _ := json.Marshal(map[string]any{
		"permissions": []string{PermObjectRead, PermActionExecute},
	})
	req := withAdmin(httptest.NewRequest(http.MethodPut, "/api/admin/roles/data-scientist/permissions", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.setPermissionsFor(rec, req, "data-scientist")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp RolePermissionsResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Permissions) != 2 {
		t.Errorf("expected 2 perms, got %v", resp.Permissions)
	}
}

func TestRoleHandler_SetPermissions_BuiltinProtected(t *testing.T) {
	h, _ := newRoleHandlerHarness()
	body, _ := json.Marshal(map[string]any{"permissions": []string{PermObjectRead}})
	req := withAdmin(httptest.NewRequest(http.MethodPut, "/api/admin/roles/admin/permissions", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.setPermissionsFor(rec, req, RoleAdmin)
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", rec.Code)
	}
}

func TestRoleHandler_Get_NotFound(t *testing.T) {
	h, _ := newRoleHandlerHarness()
	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/roles/ghost", nil))
	rec := httptest.NewRecorder()
	h.getFor(rec, req, "ghost")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestRoleHandler_Update_Description(t *testing.T) {
	h, _ := newRoleHandlerHarness()
	desc := "updated viewer description"
	body, _ := json.Marshal(map[string]any{"description": desc})
	req := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/roles/viewer", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.updateFor(rec, req, RoleViewer)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp RoleResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Description != desc {
		t.Errorf("got desc %q", resp.Description)
	}
}

func TestRoleHandler_RequiresAuth(t *testing.T) {
	h, _ := newRoleHandlerHarness()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/roles", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
