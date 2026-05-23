package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestBDD_RoleAdminWritesRejectAmbiguousJSONBody_P2A301 covers the three
// RBAC role admin write surfaces:
//
//   - POST   /api/admin/roles                       (Create)
//   - PATCH  /api/admin/roles/{name}                (Update)
//   - PUT    /api/admin/roles/{name}/permissions    (SetPermissions)
//
// For each surface it asserts that a body composed of two concatenated JSON
// objects is rejected with HTTP 400 plus a "single JSON value" reason, and
// that the underlying repository state is not mutated by the rejected
// request. A trailing well-formed regression sub-test confirms the existing
// happy paths still succeed after the hardening.
func TestBDD_RoleAdminWritesRejectAmbiguousJSONBody_P2A301(t *testing.T) {
	t.Run("Create rejects concatenated JSON without inserting a role", func(t *testing.T) {
		h, repo := newRoleHandlerHarness()
		seedRoles := snapshotRoleNames(repo)

		body := `{"name":"data-scientist","description":"safe","permissions":["object:read"]}` +
			`{"name":"smuggled","permissions":["user:manage"]}`
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/roles", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.Create(rec, req)

		assertSingleJSONValueRejection(t, rec, "InvalidRoleRequest")

		afterRoles := snapshotRoleNames(repo)
		if !reflect.DeepEqual(afterRoles, seedRoles) {
			t.Fatalf("Create with concatenated body mutated role set: before=%v after=%v", seedRoles, afterRoles)
		}
		repo.mu.Lock()
		if _, ok := repo.permissions["data-scientist"]; ok {
			t.Fatalf("Create with concatenated body persisted permissions for data-scientist")
		}
		if _, ok := repo.permissions["smuggled"]; ok {
			t.Fatalf("Create with concatenated body persisted permissions for smuggled role")
		}
		repo.mu.Unlock()
	})

	t.Run("Update rejects concatenated JSON without changing description", func(t *testing.T) {
		h, repo := newRoleHandlerHarness()
		if err := repo.Create(context.Background(), &Role{Name: "analyst", Description: "original"}); err != nil {
			t.Fatalf("seed analyst role: %v", err)
		}

		newDesc := "first-decoded"
		first, err := json.Marshal(RoleUpdateRequest{Description: &newDesc})
		if err != nil {
			t.Fatalf("marshal first patch: %v", err)
		}
		body := string(first) + `{"description":"smuggled-second"}`
		req := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/roles/analyst", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.updateFor(rec, req, "analyst")

		assertSingleJSONValueRejection(t, rec, "InvalidRoleUpdate")

		got, err := repo.Get(context.Background(), "analyst")
		if err != nil {
			t.Fatalf("re-read analyst: %v", err)
		}
		if got.Description != "original" {
			t.Fatalf("ambiguous Update mutated description to %q (want %q)", got.Description, "original")
		}
	})

	t.Run("SetPermissions rejects concatenated JSON without changing permissions", func(t *testing.T) {
		h, repo := newRoleHandlerHarness()
		if err := repo.Create(context.Background(), &Role{Name: "analyst"}); err != nil {
			t.Fatalf("seed analyst role: %v", err)
		}
		baseline := []string{PermObjectRead}
		if err := repo.SetPermissions(context.Background(), "analyst", baseline); err != nil {
			t.Fatalf("seed permissions: %v", err)
		}

		body := `{"permissions":["object:read","action:execute"]}` +
			`{"permissions":["user:manage"]}`
		req := withAdmin(httptest.NewRequest(http.MethodPut, "/api/admin/roles/analyst/permissions", strings.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.setPermissionsFor(rec, req, "analyst")

		assertSingleJSONValueRejection(t, rec, "InvalidPermissionsRequest")

		got, err := repo.ListPermissions(context.Background(), "analyst")
		if err != nil {
			t.Fatalf("re-read analyst permissions: %v", err)
		}
		sort.Strings(got)
		sort.Strings(baseline)
		if !reflect.DeepEqual(got, baseline) {
			t.Fatalf("ambiguous SetPermissions mutated permissions: got %v want %v", got, baseline)
		}
	})

	t.Run("well-formed bodies still succeed across all three surfaces", func(t *testing.T) {
		h, repo := newRoleHandlerHarness()

		// Create happy path.
		createBody, _ := json.Marshal(map[string]any{
			"name":        "analyst",
			"description": "ML team",
			"permissions": []string{PermObjectRead, PermActionExecute},
		})
		createReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/roles", bytes.NewReader(createBody)))
		createReq.Header.Set("Content-Type", "application/json")
		createRec := httptest.NewRecorder()
		h.Create(createRec, createReq)
		if createRec.Code != http.StatusCreated {
			t.Fatalf("happy Create returned %d body=%s", createRec.Code, createRec.Body.String())
		}

		// Update happy path.
		desc := "renamed"
		updateBody, _ := json.Marshal(RoleUpdateRequest{Description: &desc})
		updateReq := withAdmin(httptest.NewRequest(http.MethodPatch, "/api/admin/roles/analyst", bytes.NewReader(updateBody)))
		updateReq.Header.Set("Content-Type", "application/json")
		updateRec := httptest.NewRecorder()
		h.updateFor(updateRec, updateReq, "analyst")
		if updateRec.Code != http.StatusOK {
			t.Fatalf("happy Update returned %d body=%s", updateRec.Code, updateRec.Body.String())
		}
		got, err := repo.Get(context.Background(), "analyst")
		if err != nil {
			t.Fatalf("re-read analyst after happy update: %v", err)
		}
		if got.Description != desc {
			t.Fatalf("happy Update did not persist new description: %q", got.Description)
		}

		// SetPermissions happy path.
		permsBody, _ := json.Marshal(map[string]any{
			"permissions": []string{PermObjectRead, PermActionExecute},
		})
		permsReq := withAdmin(httptest.NewRequest(http.MethodPut, "/api/admin/roles/analyst/permissions", bytes.NewReader(permsBody)))
		permsReq.Header.Set("Content-Type", "application/json")
		permsRec := httptest.NewRecorder()
		h.setPermissionsFor(permsRec, permsReq, "analyst")
		if permsRec.Code != http.StatusOK {
			t.Fatalf("happy SetPermissions returned %d body=%s", permsRec.Code, permsRec.Body.String())
		}
		gotPerms, err := repo.ListPermissions(context.Background(), "analyst")
		if err != nil {
			t.Fatalf("re-read analyst permissions after happy set: %v", err)
		}
		sort.Strings(gotPerms)
		want := []string{PermActionExecute, PermObjectRead}
		sort.Strings(want)
		if !reflect.DeepEqual(gotPerms, want) {
			t.Fatalf("happy SetPermissions: got %v want %v", gotPerms, want)
		}
	})
}

func snapshotRoleNames(repo *fakeRoleRepo) []string {
	repo.mu.Lock()
	defer repo.mu.Unlock()
	out := make([]string, 0, len(repo.roles))
	for name := range repo.roles {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func assertSingleJSONValueRejection(t *testing.T, rec *httptest.ResponseRecorder, wantErrorName string) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for ambiguous body, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if resp.ErrorName != wantErrorName {
		t.Fatalf("errorName: got %q, want %q", resp.ErrorName, wantErrorName)
	}
	if !strings.Contains(strings.ToLower(resp.Parameters["reason"]), "single json value") {
		t.Fatalf("reason should mention single JSON value, got %q", resp.Parameters["reason"])
	}
}
