package auth

import (
	"context"
	"testing"
	"time"
)

func TestMemoryRefreshStore_CreateAndGet(t *testing.T) {
	store := NewMemoryRefreshStore()
	ctx := context.Background()

	tok := &RefreshTokenRecord{
		ID:        "id-1",
		UserID:    "user:alice",
		TokenHash: "hash-aaa",
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := store.Create(ctx, tok); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := store.GetByHash(ctx, "hash-aaa")
	if err != nil {
		t.Fatalf("GetByHash: %v", err)
	}
	if got.UserID != "user:alice" {
		t.Errorf("UserID: got %q", got.UserID)
	}
	if got.RevokedAt != nil {
		t.Error("expected RevokedAt nil for fresh token")
	}
}

func TestMemoryRefreshStore_GetMissing(t *testing.T) {
	store := NewMemoryRefreshStore()
	_, err := store.GetByHash(context.Background(), "nope")
	if err != ErrRefreshTokenNotFound {
		t.Errorf("expected ErrRefreshTokenNotFound, got %v", err)
	}
}

func TestMemoryRefreshStore_Revoke(t *testing.T) {
	store := NewMemoryRefreshStore()
	ctx := context.Background()
	tok := &RefreshTokenRecord{
		ID:        "id-1",
		UserID:    "user:alice",
		TokenHash: "hash-aaa",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Create(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, "id-1", "manual"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	got, _ := store.GetByHash(ctx, "hash-aaa")
	if got.RevokedAt == nil {
		t.Error("expected RevokedAt to be set after revoke")
	}
	if got.RevocationReason != "manual" {
		t.Errorf("RevocationReason: got %q", got.RevocationReason)
	}
}

func TestMemoryRefreshStore_RevokeChain(t *testing.T) {
	store := NewMemoryRefreshStore()
	ctx := context.Background()

	// Create a chain of three tokens for the same user.
	root := &RefreshTokenRecord{ID: "root", UserID: "user:alice", TokenHash: "h-root", ExpiresAt: time.Now().Add(time.Hour)}
	child1 := &RefreshTokenRecord{ID: "c1", UserID: "user:alice", TokenHash: "h-c1", ExpiresAt: time.Now().Add(time.Hour), ParentID: "root"}
	child2 := &RefreshTokenRecord{ID: "c2", UserID: "user:alice", TokenHash: "h-c2", ExpiresAt: time.Now().Add(time.Hour), ParentID: "c1"}
	other := &RefreshTokenRecord{ID: "x", UserID: "user:bob", TokenHash: "h-x", ExpiresAt: time.Now().Add(time.Hour)}
	for _, tok := range []*RefreshTokenRecord{root, child1, child2, other} {
		if err := store.Create(ctx, tok); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.RevokeChainForUser(ctx, "user:alice", "reuse_detected"); err != nil {
		t.Fatalf("RevokeChainForUser: %v", err)
	}

	for _, hash := range []string{"h-root", "h-c1", "h-c2"} {
		got, _ := store.GetByHash(ctx, hash)
		if got.RevokedAt == nil {
			t.Errorf("expected %s revoked", hash)
		}
		if got.RevocationReason != "reuse_detected" {
			t.Errorf("%s reason: got %q", hash, got.RevocationReason)
		}
	}
	// Other user untouched.
	gotOther, _ := store.GetByHash(ctx, "h-x")
	if gotOther.RevokedAt != nil {
		t.Error("other user's token should not be revoked")
	}
}

func TestMemoryRefreshStore_RevokeAllForUser(t *testing.T) {
	store := NewMemoryRefreshStore()
	ctx := context.Background()
	a := &RefreshTokenRecord{ID: "a", UserID: "user:alice", TokenHash: "h-a", ExpiresAt: time.Now().Add(time.Hour)}
	b := &RefreshTokenRecord{ID: "b", UserID: "user:alice", TokenHash: "h-b", ExpiresAt: time.Now().Add(time.Hour)}
	c := &RefreshTokenRecord{ID: "c", UserID: "user:bob", TokenHash: "h-c", ExpiresAt: time.Now().Add(time.Hour)}
	for _, tok := range []*RefreshTokenRecord{a, b, c} {
		_ = store.Create(ctx, tok)
	}
	if err := store.RevokeAllForUser(ctx, "user:alice", "logout_all"); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"h-a", "h-b"} {
		got, _ := store.GetByHash(ctx, h)
		if got.RevokedAt == nil {
			t.Errorf("expected %s revoked", h)
		}
	}
	gotC, _ := store.GetByHash(ctx, "h-c")
	if gotC.RevokedAt != nil {
		t.Error("bob's token should not be revoked")
	}
}
