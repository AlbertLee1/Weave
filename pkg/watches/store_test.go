package watches

import (
	"context"
	"testing"
)

func TestMemoryStore_CreateListDeleteIsWatching(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	row := &Watch{ID: "w1", UserID: "user:alice", TargetRID: "ri.weave.main.object.42"}
	if err := s.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if row.CreatedAt.IsZero() {
		t.Fatalf("Create did not stamp CreatedAt")
	}

	// Idempotent re-Create with a different proposed id returns the
	// existing row.
	dup := &Watch{ID: "w2", UserID: "user:alice", TargetRID: "ri.weave.main.object.42"}
	if err := s.Create(ctx, dup); err != nil {
		t.Fatalf("Create dup: %v", err)
	}
	if dup.ID != "w1" {
		t.Fatalf("Idempotent re-create should return existing row id, got %s", dup.ID)
	}

	// IsWatching=true for owner.
	got, err := s.IsWatching(ctx, "user:alice", "ri.weave.main.object.42")
	if err != nil || !got {
		t.Fatalf("IsWatching want true,nil got %v,%v", got, err)
	}
	// IsWatching=false for cross-user.
	got, err = s.IsWatching(ctx, "user:bob", "ri.weave.main.object.42")
	if err != nil || got {
		t.Fatalf("Cross-user IsWatching want false,nil got %v,%v", got, err)
	}
	// IsWatching=false for unknown target.
	got, err = s.IsWatching(ctx, "user:alice", "ri.weave.main.object.99")
	if err != nil || got {
		t.Fatalf("Unknown target IsWatching want false,nil got %v,%v", got, err)
	}

	// Add a second watch for alice, one for bob.
	if err := s.Create(ctx, &Watch{ID: "w3", UserID: "user:alice", TargetRID: "ri.weave.main.object.7"}); err != nil {
		t.Fatalf("Create #2: %v", err)
	}
	if err := s.Create(ctx, &Watch{ID: "w4", UserID: "user:bob", TargetRID: "ri.weave.main.object.42"}); err != nil {
		t.Fatalf("Create bob: %v", err)
	}

	rows, err := s.List(ctx, "user:alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("Alice should see 2 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.UserID != "user:alice" {
			t.Fatalf("List leaked another user's row: %+v", r)
		}
	}

	// Delete alice's first watch.
	if err := s.Delete(ctx, "user:alice", "ri.weave.main.object.42"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = s.IsWatching(ctx, "user:alice", "ri.weave.main.object.42")
	if got {
		t.Fatalf("Post-delete IsWatching should be false")
	}
	// Delete the same row again → ErrNotFound.
	if err := s.Delete(ctx, "user:alice", "ri.weave.main.object.42"); err != ErrNotFound {
		t.Fatalf("Re-delete: want ErrNotFound, got %v", err)
	}
	// Bob's row for the same target is untouched.
	got, _ = s.IsWatching(ctx, "user:bob", "ri.weave.main.object.42")
	if !got {
		t.Fatalf("Bob's parallel watch should survive alice's delete")
	}
}

func TestValidateTargetRID(t *testing.T) {
	cases := []struct {
		name   string
		target string
		ok     bool
	}{
		{"valid", "ri.weave.main.object.42", true},
		{"empty", "", false},
		{"whitespace", "   ", false},
		{"not a rid", "object-42", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTargetRID(tc.target)
			if tc.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("want error for %q", tc.target)
			}
		})
	}
}
