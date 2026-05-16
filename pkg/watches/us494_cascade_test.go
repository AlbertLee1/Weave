package watches

import (
	"context"
	"testing"
)

// TestMemoryStore_DeleteAllForUser_RemovesEveryFollow pins the US-494
// cascade-erase contract: every (user, target) follow row owned by
// userID is hard-deleted; other users untouched.
func TestMemoryStore_DeleteAllForUser_RemovesEveryFollow(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.Create(ctx, &Watch{ID: "w1", UserID: "alice", TargetRID: "ri.x.y.z.a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &Watch{ID: "w2", UserID: "alice", TargetRID: "ri.x.y.z.b"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &Watch{ID: "w3", UserID: "bob", TargetRID: "ri.x.y.z.a"}); err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteAllForUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("rows affected = %d, want 2", n)
	}

	out, err := s.List(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Errorf("alice still has %d watches: %#v", len(out), out)
	}
	bob, _ := s.List(ctx, "bob")
	if len(bob) != 1 {
		t.Errorf("bob row was clobbered: %#v", bob)
	}
}

func TestMemoryStore_DeleteAllForUser_IdempotentEmpty(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.DeleteAllForUser(context.Background(), "ghost"); err != nil {
		t.Fatal(err)
	}
}
