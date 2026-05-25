package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestBDD_SessionHandlerRevokeOthers covers round 101 — bulk session
// revocation, the "log out other devices" security flow. Mirrors
// Foundry's same-named endpoint.
//
// Endpoint:  POST /api/auth/sessions/revoke-others
// Response:  {"revoked": N, "currentSessionId": "..."}
//
// Invariants:
//   - 401 when unauthenticated
//   - Current session (per SessionIDAttributeKey on User.Attributes)
//     is NEVER revoked when anchor is present
//   - All other sessions belonging to the caller are revoked
//   - Sessions belonging to OTHER users are never touched (cross-user
//     leak guard — uses Delete which already enforces caller ownership)
//   - When caller has no current-session anchor (API-key auth),
//     ALL of the caller's sessions are revoked
//   - Revoked count matches the actual number of deleted rows
func TestBDD_SessionHandlerRevokeOthers(t *testing.T) {
	t.Run("Unauthenticated returns 401", func(t *testing.T) {
		h, _ := newSessionHandler(t)
		req := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/revoke-others", nil)
		rec := httptest.NewRecorder()
		h.RevokeOthers(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d, want 401", rec.Code)
		}
	})

	t.Run("Revokes all but current session when anchor present", func(t *testing.T) {
		h, store := newSessionHandler(t)
		ctx := context.Background()
		// Alice has 3 sessions; s2 is current.
		_ = store.Create(ctx, &SessionRecord{ID: "s1", UserID: "user:alice", LastSeen: time.Unix(100, 0)})
		_ = store.Create(ctx, &SessionRecord{ID: "s2", UserID: "user:alice", LastSeen: time.Unix(200, 0)})
		_ = store.Create(ctx, &SessionRecord{ID: "s3", UserID: "user:alice", LastSeen: time.Unix(150, 0)})
		// Bob has his own session — must not be touched.
		_ = store.Create(ctx, &SessionRecord{ID: "b1", UserID: "user:bob"})

		req := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/revoke-others", nil)
		req = req.WithContext(WithUser(req.Context(), &User{
			ID:         "user:alice",
			Attributes: map[string]any{SessionIDAttributeKey: "s2"},
		}))
		rec := httptest.NewRecorder()
		h.RevokeOthers(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp RevokeOthersResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Revoked != 2 {
			t.Errorf("Revoked=%d, want 2 (s1 + s3)", resp.Revoked)
		}
		if resp.CurrentSessionID != "s2" {
			t.Errorf("CurrentSessionID=%q, want s2", resp.CurrentSessionID)
		}

		// Confirm s2 still present, s1/s3 gone, b1 untouched.
		if _, err := store.Get(ctx, "s2"); err != nil {
			t.Errorf("current session s2 was revoked: %v", err)
		}
		if _, err := store.Get(ctx, "s1"); err == nil {
			t.Errorf("s1 should be revoked but still exists")
		}
		if _, err := store.Get(ctx, "s3"); err == nil {
			t.Errorf("s3 should be revoked but still exists")
		}
		if _, err := store.Get(ctx, "b1"); err != nil {
			t.Errorf("bob's session b1 must NOT be touched: %v", err)
		}
	})

	t.Run("Zero other sessions returns revoked=0", func(t *testing.T) {
		h, store := newSessionHandler(t)
		ctx := context.Background()
		_ = store.Create(ctx, &SessionRecord{ID: "only-one", UserID: "user:alice"})

		req := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/revoke-others", nil)
		req = req.WithContext(WithUser(req.Context(), &User{
			ID:         "user:alice",
			Attributes: map[string]any{SessionIDAttributeKey: "only-one"},
		}))
		rec := httptest.NewRecorder()
		h.RevokeOthers(rec, req)

		var resp RevokeOthersResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Revoked != 0 {
			t.Errorf("Revoked=%d, want 0", resp.Revoked)
		}
		if resp.CurrentSessionID != "only-one" {
			t.Errorf("CurrentSessionID=%q, want only-one", resp.CurrentSessionID)
		}
		if _, err := store.Get(ctx, "only-one"); err != nil {
			t.Errorf("only-one should still exist: %v", err)
		}
	})

	t.Run("API-key auth (no anchor) revokes ALL caller sessions", func(t *testing.T) {
		h, store := newSessionHandler(t)
		ctx := context.Background()
		_ = store.Create(ctx, &SessionRecord{ID: "s1", UserID: "user:alice"})
		_ = store.Create(ctx, &SessionRecord{ID: "s2", UserID: "user:alice"})

		req := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/revoke-others", nil)
		// No SessionIDAttributeKey on Attributes — API-key auth path.
		req = req.WithContext(WithUser(req.Context(), &User{ID: "user:alice"}))
		rec := httptest.NewRecorder()
		h.RevokeOthers(rec, req)

		var resp RevokeOthersResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Revoked != 2 {
			t.Errorf("Revoked=%d, want 2 (no anchor → revoke all)", resp.Revoked)
		}
		if resp.CurrentSessionID != "" {
			t.Errorf("CurrentSessionID=%q, want empty (no anchor)", resp.CurrentSessionID)
		}
		if _, err := store.Get(ctx, "s1"); err == nil {
			t.Errorf("s1 should be revoked")
		}
		if _, err := store.Get(ctx, "s2"); err == nil {
			t.Errorf("s2 should be revoked")
		}
	})

	t.Run("Sessions of other users are never touched", func(t *testing.T) {
		h, store := newSessionHandler(t)
		ctx := context.Background()
		_ = store.Create(ctx, &SessionRecord{ID: "alice1", UserID: "user:alice"})
		_ = store.Create(ctx, &SessionRecord{ID: "alice2", UserID: "user:alice"})
		_ = store.Create(ctx, &SessionRecord{ID: "bob1", UserID: "user:bob"})
		_ = store.Create(ctx, &SessionRecord{ID: "bob2", UserID: "user:bob"})

		req := httptest.NewRequest(http.MethodPost, "/api/auth/sessions/revoke-others", nil)
		req = req.WithContext(WithUser(req.Context(), &User{
			ID:         "user:alice",
			Attributes: map[string]any{SessionIDAttributeKey: "alice1"},
		}))
		rec := httptest.NewRecorder()
		h.RevokeOthers(rec, req)

		// Bob's sessions must survive intact.
		if _, err := store.Get(ctx, "bob1"); err != nil {
			t.Errorf("cross-user leak: bob1 deleted: %v", err)
		}
		if _, err := store.Get(ctx, "bob2"); err != nil {
			t.Errorf("cross-user leak: bob2 deleted: %v", err)
		}
		// Alice: alice1 preserved (current), alice2 revoked.
		if _, err := store.Get(ctx, "alice1"); err != nil {
			t.Errorf("alice1 (current) was revoked: %v", err)
		}
		if _, err := store.Get(ctx, "alice2"); err == nil {
			t.Errorf("alice2 should be revoked")
		}
	})
}
