package userprefs

import (
	"context"
	"errors"
	"testing"
)

// TestMemoryStore_DeleteAllForUser_RemovesPreferencesRow verifies the
// US-494 cascade-erase contract for user preferences: after
// DeleteAllForUser, Get returns ErrNotFound — the user_id PK row is
// gone, no shadow defaults survive.
func TestMemoryStore_DeleteAllForUser_RemovesPreferencesRow(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	theme := "dark"
	if _, err := s.Upsert(ctx, "alice", Update{Theme: &theme}); err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteAllForUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("rows affected = %d, want 1", n)
	}

	if _, err := s.Get(ctx, "alice"); !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after cascade, got %v", err)
	}
}

func TestMemoryStore_DeleteAllForUser_IdempotentEmpty(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.DeleteAllForUser(context.Background(), "ghost"); err != nil {
		t.Fatal(err)
	}
}
