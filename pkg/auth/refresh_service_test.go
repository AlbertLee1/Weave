package auth

import (
	"context"
	"testing"
	"time"
)

func newTestRefreshService(t *testing.T) *RefreshService {
	t.Helper()
	store := NewMemoryRefreshStore()
	return NewRefreshService(store, RefreshServiceOptions{
		AbsoluteTTL: 7 * 24 * time.Hour,
	})
}

func TestRefreshService_Generate(t *testing.T) {
	svc := newTestRefreshService(t)
	plain, rec, err := svc.Generate(context.Background(), "user:alice", "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if plain == "" {
		t.Fatal("expected non-empty plaintext")
	}
	if len(plain) < 30 {
		t.Errorf("expected ~43 char base64url, got %d", len(plain))
	}
	if rec.UserID != "user:alice" {
		t.Errorf("UserID: got %q", rec.UserID)
	}
	if rec.TokenHash == "" {
		t.Error("expected token hash")
	}
	if rec.TokenHash == plain {
		t.Error("hash must not equal plaintext")
	}
	if rec.ExpiresAt.Before(time.Now().Add(6 * 24 * time.Hour)) {
		t.Errorf("expected expiry ~7 days out, got %v", rec.ExpiresAt)
	}
}

func TestRefreshService_LookupValid(t *testing.T) {
	svc := newTestRefreshService(t)
	plain, _, err := svc.Generate(context.Background(), "user:alice", "")
	if err != nil {
		t.Fatal(err)
	}

	rec, err := svc.Lookup(context.Background(), plain)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if rec.UserID != "user:alice" {
		t.Errorf("got %q", rec.UserID)
	}
}

func TestRefreshService_LookupInvalid(t *testing.T) {
	svc := newTestRefreshService(t)
	_, err := svc.Lookup(context.Background(), "not-a-real-token")
	if err == nil {
		t.Error("expected error for unknown token")
	}
}

func TestRefreshService_RotateHappyPath(t *testing.T) {
	svc := newTestRefreshService(t)
	ctx := context.Background()

	plainOld, oldRec, err := svc.Generate(ctx, "user:alice", "")
	if err != nil {
		t.Fatal(err)
	}

	plainNew, newRec, err := svc.Rotate(ctx, plainOld)
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if plainNew == "" || plainNew == plainOld {
		t.Error("expected new distinct plaintext")
	}
	if newRec.UserID != "user:alice" {
		t.Errorf("UserID: got %q", newRec.UserID)
	}
	if newRec.ParentID != oldRec.ID {
		t.Errorf("ParentID: got %q want %q", newRec.ParentID, oldRec.ID)
	}

	// Old token must now be revoked.
	old, _ := svc.Lookup(ctx, plainOld)
	if old.RevokedAt == nil {
		t.Error("expected old token revoked after rotation")
	}
}

func TestRefreshService_RotateExpired(t *testing.T) {
	svc := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{
		AbsoluteTTL: -1 * time.Minute, // already expired
	})
	plain, _, err := svc.Generate(context.Background(), "user:alice", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.Rotate(context.Background(), plain)
	if err != ErrRefreshTokenExpired {
		t.Errorf("expected ErrRefreshTokenExpired, got %v", err)
	}
}

func TestRefreshService_RotateAlreadyRevokedTriggersReuseDetection(t *testing.T) {
	// Per RFC 9700: presenting any revoked refresh token, regardless of how
	// it was revoked, is treated as theft and burns the entire chain.
	svc := newTestRefreshService(t)
	ctx := context.Background()
	plain, rec, err := svc.Generate(ctx, "user:alice", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.store.Revoke(ctx, rec.ID, "manual")

	_, _, err = svc.Rotate(ctx, plain)
	if err != ErrRefreshTokenReuseDetected {
		t.Errorf("expected ErrRefreshTokenReuseDetected, got %v", err)
	}
}

func TestRefreshService_ReuseDetectionKillsChain(t *testing.T) {
	svc := newTestRefreshService(t)
	ctx := context.Background()

	plain1, _, err := svc.Generate(ctx, "user:alice", "")
	if err != nil {
		t.Fatal(err)
	}
	// Rotate once: plain1 -> plain2.
	plain2, _, err := svc.Rotate(ctx, plain1)
	if err != nil {
		t.Fatal(err)
	}
	// Re-using plain1 (already revoked from first rotation) must trigger
	// chain kill: both plain1 and plain2 are dead.
	_, _, err = svc.Rotate(ctx, plain1)
	if err != ErrRefreshTokenReuseDetected {
		t.Errorf("expected ErrRefreshTokenReuseDetected, got %v", err)
	}

	// plain2 must now also be revoked.
	rec2, _ := svc.Lookup(ctx, plain2)
	if rec2.RevokedAt == nil {
		t.Error("expected plain2 revoked by chain kill")
	}
}

func TestRefreshService_HashStable(t *testing.T) {
	// Plaintext -> hash should be deterministic so the lookup can index by hash.
	a := HashRefreshToken("hello")
	b := HashRefreshToken("hello")
	if a != b {
		t.Error("hash must be deterministic")
	}
	if HashRefreshToken("hello") == HashRefreshToken("world") {
		t.Error("different inputs must hash differently")
	}
}
