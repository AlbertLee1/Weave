package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBDD_MeHandler_PopulatesEmailAndName covers a Foundry-1:1
// alignment bug on the /api/v2/me wire shape. MeResponse declares
// `email` and `name` fields in its JSON tags, but the original
// MeHandler implementation never populated them — the struct
// literal at the bottom set only ID / Roles / OntologyRoles /
// Permissions / Markings, so both Email and Name silently
// serialised as `""` even when the authenticated User carried
// non-empty values.
//
// Symptom: an SDK rendering "Welcome, {user.name}" on a dashboard
// shows "Welcome, " (empty string). An auth modal that wants to
// surface the caller's email for confirmation also shows nothing.
// Foundry's equivalent endpoint always echoes these fields, and
// the existing TestMeHandler_DevAdmin / TestMeHandler_Viewer
// suites silently accept the bug because they decode the fields
// but never assert on their values.
//
// The fix is one-line: populate Email + Name from u.Email + u.Name
// in MeHandler's response struct.
func TestBDD_MeHandler_PopulatesEmailAndName(t *testing.T) {
	t.Run("Email + Name from User round-trip into the response body", func(t *testing.T) {
		h := MeHandler()
		req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
		ctx := WithUser(req.Context(), &User{
			ID:    "user:alice@example.com",
			Email: "alice@example.com",
			Name:  "Alice",
			Roles: []string{RoleViewer},
		})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var got struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		}
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.ID != "user:alice@example.com" {
			t.Errorf("id: got %q, want user:alice@example.com", got.ID)
		}
		if got.Email != "alice@example.com" {
			t.Errorf("email: got %q, want alice@example.com (MeHandler must populate Email from u.Email)", got.Email)
		}
		if got.Name != "Alice" {
			t.Errorf("name: got %q, want Alice (MeHandler must populate Name from u.Name)", got.Name)
		}
	})

	t.Run("Empty Email + Name still serialize as empty strings (back-compat)", func(t *testing.T) {
		// A degraded-mode caller with no email / name on file (e.g.
		// dev mode admin) must still produce `"email":""`,
		// `"name":""` — not omit them — so SDK type definitions
		// can keep the fields as required strings rather than
		// optional pointers.
		h := MeHandler()
		req := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
		ctx := WithUser(req.Context(), &User{
			ID:    "dev-user",
			Roles: []string{RoleAdmin},
		})
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		body := rec.Body.String()
		// Both keys MUST be present in the wire even when blank.
		// Confirming via raw substring match because the json
		// decoder would silently default missing keys to "".
		for _, key := range []string{`"email":`, `"name":`} {
			if !contains(body, key) {
				t.Errorf("body missing %s key; got %s", key, body)
			}
		}
	})
}

// contains is a small strings.Contains shim to keep the import block tight.
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
