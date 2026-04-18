package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/audit"
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

func TestAPIKeyHandler_Rotate_201_ReturnsNewRawKey(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	// Seed a key via Create to mirror the production shape.
	createBody, _ := json.Marshal(map[string]any{"name": "ci-bot", "scopes": []string{"read"}})
	createReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(createBody)))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	var created APIKeyCreateResponse
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}

	// Rotate with no body: defaults to 7-day grace.
	rotReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+created.ID+"/rotate", nil))
	rotRec := httptest.NewRecorder()
	h.RotateFor(rotRec, rotReq, created.ID)

	if rotRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", rotRec.Code, rotRec.Body.String())
	}

	var resp APIKeyRotateResponse
	if err := json.NewDecoder(rotRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode rotate: %v", err)
	}
	if resp.RawKey == "" || !strings.HasPrefix(resp.RawKey, "wvk_") {
		t.Errorf("expected successor rawKey returned once, got %q", resp.RawKey)
	}
	if resp.PredecessorID != created.ID {
		t.Errorf("PredecessorID: got %q, want %q", resp.PredecessorID, created.ID)
	}
	if resp.ID == "" || resp.ID == created.ID {
		t.Errorf("successor ID must be fresh, got %q", resp.ID)
	}
	if resp.Name != "ci-bot" {
		t.Errorf("Name inherited from predecessor; got %q", resp.Name)
	}
	if len(resp.Scopes) != 1 || resp.Scopes[0] != "read" {
		t.Errorf("Scopes inherited from predecessor; got %v", resp.Scopes)
	}
	// Grace is ~7 days in the future (allow 1-minute drift for test runtime).
	expected := time.Now().Add(DefaultAPIKeyRotationGrace)
	delta := resp.PredecessorExpiry.Sub(expected)
	if delta < -time.Minute || delta > time.Minute {
		t.Errorf("PredecessorExpiry: got %v, expected ~%v (delta %v)", resp.PredecessorExpiry, expected, delta)
	}

	// Predecessor row now carries rotates_at and successor_id.
	pred, err := repo.GetByID(rotReq.Context(), created.ID)
	if err != nil {
		t.Fatalf("GetByID predecessor: %v", err)
	}
	if pred.RotatesAt == nil {
		t.Error("expected predecessor.RotatesAt populated")
	}
	if pred.SuccessorID == nil || *pred.SuccessorID != resp.ID {
		t.Errorf("expected predecessor.SuccessorID == %q, got %v", resp.ID, pred.SuccessorID)
	}

	// Successor is live in its own right.
	succ, err := repo.GetByPrefix(rotReq.Context(), resp.Prefix)
	if err != nil {
		t.Fatalf("GetByPrefix successor: %v", err)
	}
	if succ.ID != resp.ID {
		t.Errorf("successor ID mismatch: %q vs %q", succ.ID, resp.ID)
	}
}

func TestAPIKeyHandler_Rotate_CustomGraceDays(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	createBody, _ := json.Marshal(map[string]any{"name": "x"})
	createReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(createBody)))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	var created APIKeyCreateResponse
	json.NewDecoder(createRec.Body).Decode(&created)

	body, _ := json.Marshal(map[string]any{"graceDays": 2})
	rotReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+created.ID+"/rotate", bytes.NewReader(body)))
	rotReq.Header.Set("Content-Type", "application/json")
	rotRec := httptest.NewRecorder()
	h.RotateFor(rotRec, rotReq, created.ID)
	if rotRec.Code != http.StatusCreated {
		t.Fatalf("status %d body=%s", rotRec.Code, rotRec.Body.String())
	}

	pred, _ := repo.GetByID(rotReq.Context(), created.ID)
	expected := time.Now().Add(2 * 24 * time.Hour)
	if pred.RotatesAt == nil {
		t.Fatal("expected RotatesAt populated")
	}
	delta := pred.RotatesAt.Sub(expected)
	if delta < -time.Minute || delta > time.Minute {
		t.Errorf("custom grace window: got rotates_at=%v, expected ~%v", pred.RotatesAt, expected)
	}
}

func TestAPIKeyHandler_Rotate_RejectsDoubleRotation_409(t *testing.T) {
	h, _ := newAPIKeyHandlerHarness(t)

	createBody, _ := json.Marshal(map[string]any{"name": "x"})
	createReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys", bytes.NewReader(createBody)))
	createRec := httptest.NewRecorder()
	h.Create(createRec, createReq)
	var created APIKeyCreateResponse
	json.NewDecoder(createRec.Body).Decode(&created)

	// First rotation: ok.
	rotReq1 := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+created.ID+"/rotate", nil))
	h.RotateFor(httptest.NewRecorder(), rotReq1, created.ID)

	// Second rotation: must 409.
	rotReq2 := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+created.ID+"/rotate", nil))
	rotRec2 := httptest.NewRecorder()
	h.RotateFor(rotRec2, rotReq2, created.ID)

	if rotRec2.Code != http.StatusConflict {
		t.Errorf("expected 409 on second rotation, got %d body=%s", rotRec2.Code, rotRec2.Body.String())
	}
}

func TestAPIKeyHandler_Rotate_RejectsOtherOwner_403(t *testing.T) {
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

	rotReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/"+other.ID+"/rotate", nil))
	rotRec := httptest.NewRecorder()
	h.RotateFor(rotRec, rotReq, other.ID)

	if rotRec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on non-owner rotate, got %d", rotRec.Code)
	}
}

func TestAPIKeyHandler_Rotate_MissingKey_404(t *testing.T) {
	h, _ := newAPIKeyHandlerHarness(t)

	rotReq := withAdmin(httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/does-not-exist/rotate", nil))
	rotRec := httptest.NewRecorder()
	h.RotateFor(rotRec, rotReq, "does-not-exist")

	if rotRec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rotRec.Code)
	}
}

func TestAPIKeyHandler_Rotate_RequiresAuth(t *testing.T) {
	h, _ := newAPIKeyHandlerHarness(t)

	rotReq := httptest.NewRequest(http.MethodPost, "/api/admin/api-keys/abc/rotate", nil)
	rotRec := httptest.NewRecorder()
	h.RotateFor(rotRec, rotReq, "abc")

	if rotRec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rotRec.Code)
	}
}

func TestAPIKeyHandler_Rotations_ListsKeysNearingRotation(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	// Seed: one key near rotation, one not rotating, one well past the window.
	adminID := "user:admin@example.com"

	// Active, no rotation: must not appear.
	rawA, prefixA, _ := GenerateAPIKey()
	repo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), &APIKeyRecord{
		KeyHash: HashAPIKey(rawA), KeyPrefix: prefixA, UserID: adminID, Name: "no-rot",
	})

	// Active, rotates in 3 days (inside default 7d window): must appear.
	rawB, prefixB, _ := GenerateAPIKey()
	nearID := "key-near"
	nearRot := time.Now().Add(3 * 24 * time.Hour)
	nearSucc := "succ-1"
	repo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), &APIKeyRecord{
		ID:          nearID,
		KeyHash:     HashAPIKey(rawB),
		KeyPrefix:   prefixB,
		UserID:      adminID,
		Name:        "near-rot",
		RotatesAt:   &nearRot,
		SuccessorID: &nearSucc,
	})

	// Active, rotates in 30 days (outside default 7d window): must NOT appear.
	rawC, prefixC, _ := GenerateAPIKey()
	farRot := time.Now().Add(30 * 24 * time.Hour)
	repo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), &APIKeyRecord{
		KeyHash: HashAPIKey(rawC), KeyPrefix: prefixC, UserID: adminID, Name: "far-rot", RotatesAt: &farRot,
	})

	// Rotating key owned by another user: must NOT leak.
	rawD, prefixD, _ := GenerateAPIKey()
	otherRot := time.Now().Add(2 * 24 * time.Hour)
	repo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), &APIKeyRecord{
		KeyHash: HashAPIKey(rawD), KeyPrefix: prefixD, UserID: "user:other@example.com", Name: "other-near", RotatesAt: &otherRot,
	})

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/api-keys/rotations", nil))
	rec := httptest.NewRecorder()
	h.Rotations(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp APIKeyRotationsResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d (%v)", len(resp.Warnings), resp.Warnings)
	}
	if resp.Warnings[0].ID != nearID {
		t.Errorf("wrong key surfaced: %+v", resp.Warnings[0])
	}
	if resp.Warnings[0].SuccessorID != nearSucc {
		t.Errorf("successor pointer should propagate: %+v", resp.Warnings[0])
	}
}

func TestAPIKeyHandler_Rotations_WithinDaysQuery(t *testing.T) {
	h, repo := newAPIKeyHandlerHarness(t)

	// Key rotates in 10 days: inside a withinDays=14 window, outside default 7d.
	raw, prefix, _ := GenerateAPIKey()
	rot := time.Now().Add(10 * 24 * time.Hour)
	repo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), &APIKeyRecord{
		KeyHash: HashAPIKey(raw), KeyPrefix: prefix, UserID: "user:admin@example.com",
		Name: "t", RotatesAt: &rot,
	})

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/api-keys/rotations?withinDays=14", nil))
	rec := httptest.NewRecorder()
	h.Rotations(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp APIKeyRotationsResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp.Warnings) != 1 {
		t.Errorf("expected widened window to surface the key, got %d warnings", len(resp.Warnings))
	}
}

func TestAPIKeyHandler_Rotations_InvalidWithinDays_400(t *testing.T) {
	h, _ := newAPIKeyHandlerHarness(t)
	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/api-keys/rotations?withinDays=abc", nil))
	rec := httptest.NewRecorder()
	h.Rotations(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAPIKeyHandler_Rotations_RequiresAuth(t *testing.T) {
	h, _ := newAPIKeyHandlerHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/api-keys/rotations", nil)
	rec := httptest.NewRecorder()
	h.Rotations(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAPIKeyHandler_Rotations_EmitsAuditEvents(t *testing.T) {
	repo := newFakeAPIKeyRepo()
	audits := audit.NewMemoryStore()
	h := NewAPIKeyHandler(repo, audits)

	rot := time.Now().Add(4 * 24 * time.Hour)
	raw, prefix, _ := GenerateAPIKey()
	repo.Create(httptest.NewRequest(http.MethodGet, "/", nil).Context(), &APIKeyRecord{
		KeyHash: HashAPIKey(raw), KeyPrefix: prefix,
		UserID: "user:admin@example.com", Name: "warn-me", RotatesAt: &rot,
	})

	req := withAdmin(httptest.NewRequest(http.MethodGet, "/api/admin/api-keys/rotations", nil))
	rec := httptest.NewRecorder()
	h.Rotations(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	// Poll audit store for at least one api_key_rotation_warning event.
	events, _ := audits.List(context.Background(), audit.ListFilter{Action: "api_key_rotation_warning"})
	if len(events) != 1 {
		t.Fatalf("expected 1 warning audit, got %d", len(events))
	}
	if events[0].ActorID != "user:admin@example.com" {
		t.Errorf("actor id: %q", events[0].ActorID)
	}
	if events[0].ResourceType != "APIKey" {
		t.Errorf("resource type: %q", events[0].ResourceType)
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
