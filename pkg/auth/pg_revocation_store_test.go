//go:build integration

package auth

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
)

// US-491 integration test: the PG-backed revocation store performs the
// indexed lookup the middleware relies on, and the reaper loop prunes
// naturally-expired rows on its cadence.

func setupRevocationStoreTest(t *testing.T) *PGRevocationStore {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("RunMigrationsUp: %v", err)
	}
	return NewPGRevocationStore(pg.Pool)
}

func TestPGRevocationStore_RevokeAndQuery_US491(t *testing.T) {
	store := setupRevocationStoreTest(t)
	ctx := context.Background()
	exp := time.Now().UTC().Add(15 * time.Minute)

	if err := store.Revoke(ctx, RevocationRecord{
		JTI:       "jti-pg-1",
		UserID:    "user:alice",
		ExpiresAt: exp,
		Reason:    "logout",
	}); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	yes, err := store.IsRevoked(ctx, "jti-pg-1")
	if err != nil {
		t.Fatalf("IsRevoked: %v", err)
	}
	if !yes {
		t.Fatal("expected jti-pg-1 to be revoked")
	}

	missing, err := store.IsRevoked(ctx, "jti-unknown")
	if err != nil {
		t.Fatalf("IsRevoked unknown: %v", err)
	}
	if missing {
		t.Fatal("unknown jti should not report revoked")
	}
}

func TestPGRevocationStore_ReapExpired_US491(t *testing.T) {
	store := setupRevocationStoreTest(t)
	ctx := context.Background()

	// Future row stays, past row is reaped.
	if err := store.Revoke(ctx, RevocationRecord{JTI: "jti-keep", ExpiresAt: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, RevocationRecord{JTI: "jti-drop", ExpiresAt: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	n, err := store.ReapExpired(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("ReapExpired: %v", err)
	}
	if n != 1 {
		t.Fatalf("reaped: got %d want 1", n)
	}
	if yes, _ := store.IsRevoked(ctx, "jti-drop"); yes {
		t.Fatal("dropped row still present")
	}
	if yes, _ := store.IsRevoked(ctx, "jti-keep"); !yes {
		t.Fatal("kept row missing")
	}
}

func TestPGRevocationStore_IdempotentRevoke_US491(t *testing.T) {
	store := setupRevocationStoreTest(t)
	ctx := context.Background()
	exp := time.Now().UTC().Add(time.Hour)
	for i := 0; i < 3; i++ {
		if err := store.Revoke(ctx, RevocationRecord{
			JTI:       "jti-dup",
			ExpiresAt: exp,
			Reason:    "iter",
		}); err != nil {
			t.Fatalf("Revoke iter %d: %v", i, err)
		}
	}
	if yes, _ := store.IsRevoked(ctx, "jti-dup"); !yes {
		t.Fatal("dup jti should still be revoked")
	}
}

func TestRunRevocationReaperLoop_TicksAndStops_US491(t *testing.T) {
	store := setupRevocationStoreTest(t)
	ctx := context.Background()
	// Seed two expired rows so the first tick reports a positive prune count.
	if err := store.Revoke(ctx, RevocationRecord{JTI: "jti-loop-1", ExpiresAt: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, RevocationRecord{JTI: "jti-loop-2", ExpiresAt: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	var totalReaped int64
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		RunRevocationReaperLoop(loopCtx, store, 20*time.Millisecond,
			func(n int64) { atomic.AddInt64(&totalReaped, n) },
			func(err error) { t.Logf("reaper error: %v", err) },
		)
		close(done)
	}()

	// Wait briefly for at least one tick; 100ms is several intervals.
	time.Sleep(150 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reaper loop did not exit after cancel")
	}
	if atomic.LoadInt64(&totalReaped) != 2 {
		t.Fatalf("totalReaped: got %d want 2", atomic.LoadInt64(&totalReaped))
	}
}
