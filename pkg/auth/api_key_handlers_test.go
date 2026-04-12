package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newAPIKeyHandlerHarness builds a fresh handler with an in-memory repo and a
// pre-seeded admin user owning all keys created in tests.
func newAPIKeyHandlerHarness(t *testing.T) (*APIKeyHandler, *fakeAPIKeyRepo) {
	t.Helper()
	repo := newFakeAPIKeyRepo()
	h := NewAPIKeyHandler(repo, nil)
	return h, repo
}

// withAdmin returns a request with an admin user attached to its context.
func withAdmin(req *http.Request) *http.Request {
	return req.WithContext(WithUser(req.Context(), &User{
		ID:    "user:admin@example.com",
		Email: "admin@example.com",
		Roles: []string{RoleAdmin},
	}))
}

// withViewer returns a request with a non-admin user attached.
func withViewer(req *http.Request) *http.Request {
	return req.WithContext(WithUser(req.Context(), &User{
		ID:    "user:viewer@example.com",
		Email: "viewer@example.com",
		Roles: []string{RoleViewer},
	}))
}

func TestAPIKeyHandler_Create_201_ReturnsRawKeyOnce(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"name": "ci-bot"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp APIKeyCreateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.RawKey == "" {
		t.Error("expected rawKey to be returned exactly once on creation")
	}
	if !strings.HasPrefix(resp.RawKey, "wvk_") {
		t.Errorf("expected rawKey to start with wvk_, got %q", resp.RawKey)
	}
	if resp.Prefix == "" {
		t.Error("expected prefix to be returned")
	}
	if resp.Name != "ci-bot" {
		t.Errorf("Name: got %q", resp.Name)
	}
	if resp.ID == "" {
		t.Error("expected id to be returned")
	}

	// Verify the row landed in the repo with the correct hash + prefix.
	row, err := repo.GetByPrefix(req.Context(), resp.Prefix)
	if err != nil {
		t.Fatalf("GetByPrefix after Create: %v", err)
	}
	if row.UserID != "user:admin@example.com" {
		t.Errorf("UserID: got %q", row.UserID)
	}
}

func TestAPIKeyHandler_Create_RequiresAuth(t *testing.T) {
	h, _ := newAPIKeyHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"name": "x"})
	req := httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAPIKeyHandler_Create_RejectsEmptyName(t *testing.T) {
	h, _ := newAPIKeyHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"name": ""})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d", rec.Code)
	}
}

func TestAPIKeyHandler_Create_AcceptsExpiresAt(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	exp := time.Now().Add(7 * 24 * time.Hour).UTC().Truncate(time.Second)
	body, _ := json.Marshal(map[string]any{
		"name":      "ci-bot",
		"expiresAt": exp.Format(time.RFC3339),
	})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp APIKeyCreateResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt in response")
	}
	row, _ := repo.GetByPrefix(req.Context(), resp.Prefix)
	if row.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt persisted")
	}
}

func TestAPIKeyHandler_List_ExcludesRawKey(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	// Seed two keys for the admin user via the handler.
	for _, name := range []string{"a", "b"} {
		body, _ := json.Marshal(map[string]any{"name": name})
		req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body)))
		rec := httptest.NewRecorder()
		h.Create(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("seed %s: %d", name, rec.Code)
		}
	}

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/api-keys", nil))
	rec := httptest.NewRecorder()
	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp APIKeyListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(resp.Keys))
	}
	for _, k := range resp.Keys {
		// We assert via reflection over the JSON: rawKey must NOT be present.
		raw, _ := json.Marshal(k)
		if strings.Contains(string(raw), "rawKey") {
			t.Errorf("List response leaked rawKey field: %s", raw)
		}
		if k.Prefix == "" {
			t.Error("List response missing prefix")
		}
		if k.ID == "" {
			t.Error("List response missing id")
		}
	}
	_ = repo // keep harness reference
}

func TestAPIKeyHandler_List_OnlyOwnerKeys(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	// Seed an admin key.
	body, _ := json.Marshal(map[string]any{"name": "admin-key"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	// Seed a key owned by a different user directly via the repo.
	raw, prefix, _ := GenerateAPIKey()
	repo.Create(req.Context(), &APIKeyRecord{
		KeyHash:   HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    "user:other@example.com",
		Name:      "other-key",
	})

	// List as admin: must NOT see the other user's key.
	req2 := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/api-keys", nil))
	rec2 := httptest.NewRecorder()
	h.List(rec2, req2)

	var resp APIKeyListResponse
	json.NewDecoder(rec2.Body).Decode(&resp)
	for _, k := range resp.Keys {
		if k.Name == "other-key" {
			t.Error("List leaked another user's key")
		}
	}
}

func TestAPIKeyHandler_Delete_SoftRevokes(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"name": "kill-me"})
	req := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	var created APIKeyCreateResponse
	json.NewDecoder(rec.Body).Decode(&created)

	delReq := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/api-keys/"+created.ID, nil))
	delRec := httptest.NewRecorder()
	h.DeleteFor(delRec, delReq, created.ID)

	if delRec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", delRec.Code)
	}

	// Direct repo lookup: the row must still exist (soft delete) but be revoked.
	row, err := repo.GetByID(req.Context(), created.ID)
	if err != nil {
		t.Fatalf("row missing after soft delete: %v", err)
	}
	if !row.IsRevoked() {
		t.Error("expected RevokedAt to be set after Delete")
	}
}

func TestAPIKeyHandler_Delete_RejectsOtherOwner(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	// Seed a key owned by another user.
	raw, prefix, _ := GenerateAPIKey()
	other := &APIKeyRecord{
		KeyHash:   HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    "user:other@example.com",
		Name:      "other",
	}
	repo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), other)

	delReq := withAdmin(httptest.NewRequest(http.MethodDelete, "/api/admin/api-keys/"+other.ID, nil))
	delRec := httptest.NewRecorder()
	h.DeleteFor(delRec, delReq, other.ID)

	if delRec.Code != http.StatusForbidden && delRec.Code != http.StatusNotFound {
		t.Errorf("expected 403/404 deleting another user's key, got %d", delRec.Code)
	}

	// Verify the row is still alive.
	row, _ := repo.GetByID(httptest.NewRequest(http.MethodGet, "/", nil).Context(), other.ID)
	if row == nil || row.IsRevoked() {
		t.Error("other user's key was inappropriately revoked")
	}
}

func TestAPIKeyHandler_Delete_RequiresAuth(t *testing.T) {
	h, _ := newAPIKeyHandlerHarness(t)

	delReq := httptest.NewRequest(http.MethodDelete, "/api/admin/api-keys/abc", nil)
	delRec := httptest.NewRecorder()
	h.DeleteFor(delRec, delReq, "abc")

	if delRec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", delRec.Code)
	}
}

// TestAPIKeyHandler_Create_RoutedToAdminOnly captures the route-level intent:
// the handler doesn't enforce admin itself (RequirePermission middleware does
// that at registration time), but a viewer-context call should still produce
// a key tied to that viewer's user id, not silently inherit admin powers.
// This test documents that the handler relies on UserFromContext, not on a
// hard-coded role check.
func TestAPIKeyHandler_Create_OwnedByCallingUser(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	body, _ := json.Marshal(map[string]any{"name": "viewer-key"})
	req := withViewer(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(body)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status: %d", rec.Code)
	}
	var resp APIKeyCreateResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	row, err := repo.GetByPrefix(req.Context(), resp.Prefix)
	if err != nil {
		t.Fatal(err)
	}
	if row.UserID != "user:viewer@example.com" {
		t.Errorf("expected key owned by calling viewer, got %q", row.UserID)
	}
}
