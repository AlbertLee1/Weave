package reactions

import (
	"context"
	"testing"
)

// TestMemoryStore_DeleteAllForUser_RemovesEveryReactionForUser pins the
// US-494 cascade-erase contract for reactions: every (user, target,
// emoji) triple owned by userID is hard-deleted.
func TestMemoryStore_DeleteAllForUser_RemovesEveryReactionForUser(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.Create(ctx, &Reaction{ID: "r1", UserID: "alice", TargetRID: "ri.x.y.z.a", Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &Reaction{ID: "r2", UserID: "alice", TargetRID: "ri.x.y.z.a", Emoji: "❤️"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &Reaction{ID: "r3", UserID: "bob", TargetRID: "ri.x.y.z.a", Emoji: "👍"}); err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteAllForUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("rows affected = %d, want 2", n)
	}

	// Aggregate-as-bob view sees only bob's row.
	out, err := s.AggregateForTarget(ctx, "bob", "ri.x.y.z.a")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Emoji != "👍" || out[0].Count != 1 {
		t.Errorf("post-cascade aggregate: %#v", out)
	}
}

func TestMemoryStore_DeleteAllForUser_IdempotentEmpty(t *testing.T) {
	s := NewMemoryStore()
	if _, err := s.DeleteAllForUser(context.Background(), "ghost"); err != nil {
		t.Fatal(err)
	}
}
