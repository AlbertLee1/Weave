package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemorySessionStore_CreateAndListByUser(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()

	s1 := &SessionRecord{
		ID:        "sess-1",
		UserID:    "user:alice",
		IP:        "10.0.0.1",
		UserAgent: "Mozilla/5.0",
		CreatedAt: time.Unix(1000, 0),
		LastSeen:  time.Unix(1000, 0),
	}
	s2 := &SessionRecord{
		ID:        "sess-2",
		UserID:    "user:alice",
		IP:        "10.0.0.2",
		UserAgent: "curl/8.0",
		CreatedAt: time.Unix(2000, 0),
		LastSeen:  time.Unix(2500, 0),
	}
	other := &SessionRecord{
		ID:        "sess-3",
		UserID:    "user:bob",
		IP:        "10.0.0.3",
		CreatedAt: time.Unix(500, 0),
		LastSeen:  time.Unix(500, 0),
	}

	for _, s := range []*SessionRecord{s1, s2, other} {
		if err := store.Create(ctx, s); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	got, err := store.ListByUser(ctx, "user:alice")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sessions for alice, got %d", len(got))
	}
	// Ordered by last_seen DESC — s2 most recent.
	if got[0].ID != "sess-2" || got[1].ID != "sess-1" {
		t.Fatalf("unexpected order: %+v", []string{got[0].ID, got[1].ID})
	}
}

func TestMemorySessionStore_CreateValidatesInputs(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()
	if err := store.Create(ctx, &SessionRecord{UserID: "u"}); err == nil {
		t.Fatal("expected error on empty ID")
	}
	if err := store.Create(ctx, &SessionRecord{ID: "x"}); err == nil {
		t.Fatal("expected error on empty UserID")
	}
}

func TestMemorySessionStore_Get(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()
	_ = store.Create(ctx, &SessionRecord{ID: "s1", UserID: "user:alice"})

	got, err := store.Get(ctx, "s1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UserID != "user:alice" {
		t.Fatalf("unexpected user: %s", got.UserID)
	}

	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestMemorySessionStore_DeleteAuthorization(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()
	_ = store.Create(ctx, &SessionRecord{ID: "s1", UserID: "user:alice"})
	_ = store.Create(ctx, &SessionRecord{ID: "s2", UserID: "user:bob"})

	// Owner can delete their own.
	if err := store.Delete(ctx, "s1", "user:alice"); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if _, err := store.Get(ctx, "s1"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected removed after delete, got %v", err)
	}

	// Non-owner is rejected.
	if err := store.Delete(ctx, "s2", "user:alice"); !errors.Is(err, ErrSessionForbidden) {
		t.Fatalf("expected ErrSessionForbidden, got %v", err)
	}

	// Missing id is not-found.
	if err := store.Delete(ctx, "nope", "user:alice"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestMemorySessionStore_Touch(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()
	s := &SessionRecord{
		ID:        "s1",
		UserID:    "u",
		CreatedAt: time.Unix(100, 0),
		LastSeen:  time.Unix(100, 0),
	}
	_ = store.Create(ctx, s)

	if err := store.Touch(ctx, "s1", time.Unix(500, 0)); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	got, _ := store.Get(ctx, "s1")
	if !got.LastSeen.Equal(time.Unix(500, 0)) {
		t.Fatalf("LastSeen not updated: %v", got.LastSeen)
	}

	if err := store.Touch(ctx, "missing", time.Unix(1000, 0)); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestMemorySessionStore_DeleteAllForUser(t *testing.T) {
	store := NewMemorySessionStore()
	ctx := context.Background()
	_ = store.Create(ctx, &SessionRecord{ID: "a1", UserID: "user:alice"})
	_ = store.Create(ctx, &SessionRecord{ID: "a2", UserID: "user:alice"})
	_ = store.Create(ctx, &SessionRecord{ID: "b1", UserID: "user:bob"})

	if err := store.DeleteAllForUser(ctx, "user:alice"); err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}
	got, _ := store.ListByUser(ctx, "user:alice")
	if len(got) != 0 {
		t.Fatalf("expected 0 alice sessions, got %d", len(got))
	}
	got, _ = store.ListByUser(ctx, "user:bob")
	if len(got) != 1 {
		t.Fatalf("expected 1 bob session, got %d", len(got))
	}
}
