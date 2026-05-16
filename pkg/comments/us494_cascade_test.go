package comments

import (
	"context"
	"testing"
)

// TestMemoryStore_DeleteAllForUser_HardRemovesAuthorRows verifies the
// US-494 GDPR cascade-erase contract: after DeleteAllForUser, no row
// authored by userID survives — neither live nor as a soft-deleted
// tombstone. The count of rows referencing the user_id column must be
// zero so the cascade-erase acceptance test ("user_id 出现次数 = 0")
// holds.
func TestMemoryStore_DeleteAllForUser_HardRemovesAuthorRows(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	target := "ri.ontology.main.object.t1"

	if err := s.Create(ctx, &Comment{ID: "c1", TargetRID: target, Author: "alice", Body: "a1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &Comment{ID: "c2", TargetRID: target, Author: "alice", Body: "a2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &Comment{ID: "c3", TargetRID: target, Author: "bob", Body: "b1"}); err != nil {
		t.Fatal(err)
	}
	// alice soft-deletes one of her own comments — cascade must still
	// remove the tombstone, otherwise the row keeps her user_id.
	if err := s.Delete(ctx, "c1", "alice"); err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteAllForUser(ctx, "alice")
	if err != nil {
		t.Fatalf("DeleteAllForUser: %v", err)
	}
	if n != 2 {
		t.Errorf("rows affected = %d, want 2", n)
	}

	// IncludeDeleted+admin scope to surface tombstones — every alice row
	// must be gone, bob untouched.
	rows, _, err := s.List(ctx, ListQuery{TargetRID: target, IncludeDeleted: true, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Author == "alice" {
			t.Errorf("alice row survived cascade: %#v", row)
		}
	}
	if len(rows) != 1 || rows[0].Author != "bob" {
		t.Errorf("expected only bob to survive, got %d rows: %#v", len(rows), rows)
	}
}

// TestMemoryStore_DeleteAllForUser_IsIdempotent matches the
// orchestrator's "retries are safe" contract: a second call with no
// rows left returns (0, nil) without an error.
func TestMemoryStore_DeleteAllForUser_IsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if _, err := s.DeleteAllForUser(ctx, "ghost"); err != nil {
		t.Fatalf("DeleteAllForUser empty: %v", err)
	}
}
