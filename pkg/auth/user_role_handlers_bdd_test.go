package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
)

// TestBDD_UserRoleGrantRejectsAmbiguousJSONBody_P2A305 covers the user-role
// grant admin write surface:
//
//   - POST /api/admin/users/{userId}/roles  (UserRoleHandler.GrantRole)
//
// The endpoint mints a role grant onto an existing user, so a body composed
// of two concatenated JSON objects must be rejected with HTTP 400 plus a
// "single JSON value" reason instead of silently dropping the trailing
// bytes. The test snapshots the user's role list before/after to prove the
// rejected request was non-mutating, and the trailing happy-path sub-test
// asserts that well-formed grants still land after the hardening.
func TestBDD_UserRoleGrantRejectsAmbiguousJSONBody_P2A305(t *testing.T) {
	t.Run("GrantRole rejects concatenated JSON without granting the role", func(t *testing.T) {
		h, users, _ := newUserRoleHandlerHarness()
		const userID = "user:alice@example.com"
		seedRoles := snapshotUserRoles(users, userID)

		// {"role":"viewer"}{"role":"admin"} — first decodes cleanly to viewer,
		// the smuggled trailer would silently elevate to admin under the
		// pre-hardened decoder.
		body := `{"role":"` + RoleViewer + `"}` + `{"role":"` + RoleAdmin + `"}`
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/"+userID+"/roles", bytes.NewReader([]byte(body))))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.grantRoleFor(rec, req, userID)

		assertSingleJSONValueRejection(t, rec, "InvalidUserRoleRequest")

		afterRoles := snapshotUserRoles(users, userID)
		if !reflect.DeepEqual(afterRoles, seedRoles) {
			t.Fatalf("GrantRole with concatenated body mutated user roles: before=%v after=%v", seedRoles, afterRoles)
		}
	})

	t.Run("well-formed body still grants the requested role", func(t *testing.T) {
		h, users, _ := newUserRoleHandlerHarness()
		const userID = "user:alice@example.com"

		body, err := json.Marshal(map[string]any{"role": RoleEditor})
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/users/"+userID+"/roles", bytes.NewReader(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.grantRoleFor(rec, req, userID)

		if rec.Code != http.StatusOK {
			t.Fatalf("happy GrantRole returned %d body=%s", rec.Code, rec.Body.String())
		}
		got, err := users.ListUserRoles(context.Background(), userID)
		if err != nil {
			t.Fatalf("ListUserRoles after happy grant: %v", err)
		}
		if len(got) != 1 || got[0] != RoleEditor {
			t.Fatalf("happy GrantRole did not persist editor: got %v", got)
		}
	})
}

// snapshotUserRoles returns a sorted copy of the roles granted to userID in
// the fake repository. The sort makes the before/after comparison stable
// against map iteration order.
func snapshotUserRoles(users *fakeUserRepo, userID string) []string {
	roles, _ := users.ListUserRoles(context.Background(), userID)
	out := append([]string(nil), roles...)
	sort.Strings(out)
	return out
}
