package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

// US-491: revocation store + cached middleware blacklist.
//
// The interface mirrors RefreshStore (in-memory + PG backed) and is consumed
// by both the admin handler that revokes a JTI and the middleware that
// rejects a previously-revoked access token.

func TestUS491_MemoryRevocationStore_RevokeThenIsRevoked(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewMemoryRevocationStore()

	exp := time.Now().Add(5 * time.Minute)
	if err := store.Revoke(ctx, RevocationRecord{JTI: "jti-1", UserID: "u1", ExpiresAt: exp, Reason: "logout"}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	yes, err := store.IsRevoked(ctx, "jti-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if !yes {
		t.Fatal("expected jti-1 to be revoked")
	}

	no, err := store.IsRevoked(ctx, "jti-other")
	if err != nil {
		t.Fatalf("IsRevoked other: %v", err)
	}
	if no {
		t.Fatal("unrevoked jti-other should not report as revoked")
	}
}

func TestUS491_MemoryRevocationStore_RejectsEmptyJTI(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewMemoryRevocationStore()
	err := store.Revoke(ctx, RevocationRecord{JTI: "", UserID: "u1", ExpiresAt: time.Now().Add(time.Minute)})
	if err == nil {
		t.Fatal("expected error revoking empty jti")
	}
	if !errors.Is(err, ErrRevocationInvalid) {
		t.Fatalf("expected ErrRevocationInvalid, got %v", err)
	}
}

func TestUS491_MemoryRevocationStore_ReapsExpired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewMemoryRevocationStore()

	past := time.Now().Add(-time.Hour)
	fut := time.Now().Add(time.Hour)
	if err := store.Revoke(ctx, RevocationRecord{JTI: "old", ExpiresAt: past}); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, RevocationRecord{JTI: "new", ExpiresAt: fut}); err != nil {
		t.Fatal(err)
	}

	n, err := store.ReapExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("ReapExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 reaped, got %d", n)
	}
	stillOld, _ := store.IsRevoked(ctx, "old")
	stillNew, _ := store.IsRevoked(ctx, "new")
	if stillOld {
		t.Fatal("expired old row should have been reaped")
	}
	if !stillNew {
		t.Fatal("unexpired new row must remain")
	}
}

func TestUS491_CachedRevocationChecker_BlocksAfterRevoke(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := NewMemoryRevocationStore()
	checker := NewCachedRevocationChecker(store, 1*time.Minute)

	if yes, _ := checker.IsRevoked(ctx, "jti-x"); yes {
		t.Fatal("unrevoked jti should pass through")
	}
	_ = store.Revoke(ctx, RevocationRecord{JTI: "jti-x", ExpiresAt: time.Now().Add(time.Hour)})
	// Cache must invalidate on Revoke so the next check sees the new state.
	checker.Invalidate("jti-x")
	yes, err := checker.IsRevoked(ctx, "jti-x")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if !yes {
		t.Fatal("expected revoked jti-x to be blocked")
	}
}

func TestUS491_CachedRevocationChecker_CachesNegativeLookups(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	calls := 0
	stub := stubRevocationStore{
		isRevokedFn: func(_ context.Context, jti string) (bool, error) {
			calls++
			return false, nil
		},
	}
	checker := NewCachedRevocationChecker(stub, 1*time.Minute)
	for i := 0; i < 5; i++ {
		if yes, _ := checker.IsRevoked(ctx, "jti-cached"); yes {
			t.Fatalf("iteration %d: should not be revoked", i)
		}
	}
	if calls != 1 {
		t.Fatalf("expected 1 store hit (cache absorbs the rest), got %d", calls)
	}
}

func TestUS491_CachedRevocationChecker_NilStore_AlwaysFalse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	checker := NewCachedRevocationChecker(nil, time.Minute)
	yes, err := checker.IsRevoked(ctx, "anything")
	if err != nil {
		t.Fatalf("nil-store checker returned error: %v", err)
	}
	if yes {
		t.Fatal("nil-store checker must report jti as not-revoked")
	}
}

type stubRevocationStore struct {
	isRevokedFn func(context.Context, string) (bool, error)
}

func (s stubRevocationStore) Revoke(context.Context, RevocationRecord) error { return nil }
func (s stubRevocationStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if s.isRevokedFn != nil {
		return s.isRevokedFn(ctx, jti)
	}
	return false, nil
}
func (s stubRevocationStore) ReapExpired(context.Context, time.Time) (int64, error) { return 0, nil }
