package comments

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func newRow(id, target, author, body, parent string) *Comment {
	return &Comment{
		ID:        id,
		TargetRID: target,
		Author:    author,
		Body:      body,
		ParentID:  parent,
	}
}

func TestMemoryStore_CreateGetListSoftDeleteEdit(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	target := "ri.ontology.main.object.t1"

	if err := s.Create(ctx, newRow("c1", target, "alice", "first", "")); err != nil {
		t.Fatalf("create top-level: %v", err)
	}
	if err := s.Create(ctx, newRow("c2", target, "bob", "second", "")); err != nil {
		t.Fatalf("create top-level 2: %v", err)
	}
	if err := s.Create(ctx, newRow("c3", target, "carol", "reply to first", "c1")); err != nil {
		t.Fatalf("create reply: %v", err)
	}

	// Reply to a different target → ErrInvalidParent
	if err := s.Create(ctx, newRow("c4", "ri.ontology.main.object.t2", "alice", "x", "c1")); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("cross-target reply: want ErrInvalidParent, got %v", err)
	}
	// Reply to a reply (depth>1) → ErrInvalidParent
	if err := s.Create(ctx, newRow("c5", target, "alice", "x", "c3")); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("reply-of-reply: want ErrInvalidParent, got %v", err)
	}
	// Reply to a non-existent parent → ErrInvalidParent
	if err := s.Create(ctx, newRow("c6", target, "alice", "x", "nope")); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("missing parent: want ErrInvalidParent, got %v", err)
	}

	// List target-scoped — three rows, sorted by createdAt ASC, with c1 first.
	rows, total, err := s.List(ctx, ListQuery{TargetRID: target, Limit: 50})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 || len(rows) != 3 {
		t.Fatalf("List target: want 3, got %d (total=%d)", len(rows), total)
	}
	if rows[0].ID != "c1" || rows[2].ID != "c3" {
		t.Fatalf("List ordering wrong: %s,%s,%s", rows[0].ID, rows[1].ID, rows[2].ID)
	}

	// List filtered to parent c1 returns only the reply.
	rows, total, err = s.List(ctx, ListQuery{TargetRID: target, ParentID: "c1", Limit: 50})
	if err != nil {
		t.Fatalf("List parent: %v", err)
	}
	if total != 1 || rows[0].ID != "c3" {
		t.Fatalf("parent-scoped List wrong: total=%d, rows=%+v", total, rows)
	}

	// Get returns a clean row.
	got, err := s.Get(ctx, "c1")
	if err != nil || got.Body != "first" || got.DeletedAt != nil {
		t.Fatalf("Get(c1) = %+v, %v", got, err)
	}

	// Edit by non-author → ErrForbidden
	body := "edited"
	if err := s.Update(ctx, "c1", "bob", Update{Body: &body}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-author edit: want ErrForbidden, got %v", err)
	}
	if err := s.Update(ctx, "c1", "alice", Update{Body: &body}); err != nil {
		t.Fatalf("author edit: %v", err)
	}
	got, _ = s.Get(ctx, "c1")
	if got.Body != "edited" {
		t.Fatalf("edit didn't persist: %q", got.Body)
	}

	// Soft-delete by non-author → ErrForbidden.
	if err := s.Delete(ctx, "c1", "bob"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-author delete: want ErrForbidden, got %v", err)
	}
	if err := s.Delete(ctx, "c1", "alice"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// After delete: Get returns the row but with empty body + DeletedAt.
	got, err = s.Get(ctx, "c1")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if got.Body != "" || got.DeletedAt == nil {
		t.Fatalf("soft-delete didn't redact body or set deletedAt: body=%q deletedAt=%v", got.Body, got.DeletedAt)
	}
	// Re-deleting → ErrNotFound (no double-delete).
	if err := s.Delete(ctx, "c1", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("re-delete: want ErrNotFound, got %v", err)
	}
	// Editing a soft-deleted row → ErrNotFound (no edit-zombies).
	if err := s.Update(ctx, "c1", "alice", Update{Body: &body}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("edit-deleted: want ErrNotFound, got %v", err)
	}

	// Reply to a soft-deleted parent is rejected.
	if err := s.Create(ctx, newRow("c7", target, "alice", "x", "c1")); !errors.Is(err, ErrInvalidParent) {
		t.Fatalf("reply-to-deleted: want ErrInvalidParent, got %v", err)
	}

	// List still surfaces the tombstone (so the SPA can render
	// [deleted]) — soft-deleted rows DON'T disappear.
	rows, total, err = s.List(ctx, ListQuery{TargetRID: target, Limit: 50})
	if err != nil {
		t.Fatalf("List with tombstone: %v", err)
	}
	if total != 3 {
		t.Fatalf("List with tombstone: want 3 rows, got %d", total)
	}
	// And the c1 row in the list has empty body but parentless reply still references it.
	for _, r := range rows {
		if r.ID == "c1" && (r.DeletedAt == nil || r.Body != "") {
			t.Fatalf("c1 should be a tombstone in list: %+v", r)
		}
	}
}

func TestMemoryStore_ListPagination(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	target := "ri.ontology.main.object.t1"
	for i := 0; i < 7; i++ {
		id := "c" + string(rune('0'+i))
		if err := s.Create(ctx, newRow(id, target, "alice", "n", "")); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	// First page of 3.
	rows, total, err := s.List(ctx, ListQuery{TargetRID: target, Limit: 3, Offset: 0})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 7 || len(rows) != 3 {
		t.Fatalf("page1: want total=7 rows=3, got total=%d rows=%d", total, len(rows))
	}
	// Second page of 3.
	rows, _, _ = s.List(ctx, ListQuery{TargetRID: target, Limit: 3, Offset: 3})
	if len(rows) != 3 {
		t.Fatalf("page2 size: %d", len(rows))
	}
	// Third page (1 row).
	rows, _, _ = s.List(ctx, ListQuery{TargetRID: target, Limit: 3, Offset: 6})
	if len(rows) != 1 {
		t.Fatalf("page3 size: %d", len(rows))
	}
	// Out-of-range offset returns empty without error.
	rows, _, err = s.List(ctx, ListQuery{TargetRID: target, Limit: 3, Offset: 99})
	if err != nil || len(rows) != 0 {
		t.Fatalf("oob offset: rows=%d err=%v", len(rows), err)
	}
}

func TestValidateBody(t *testing.T) {
	cases := map[string]bool{
		"":                                    false,
		"   ":                                 false,
		"x":                                   true,
		"hello world":                         true,
		strings.Repeat("a", MaxBodyLength):    true,
		strings.Repeat("a", MaxBodyLength+1):  false,
	}
	for body, valid := range cases {
		err := ValidateBody(body)
		if valid && err != nil {
			t.Errorf("ValidateBody(len=%d): unexpected error %v", len(body), err)
		}
		if !valid && err == nil {
			t.Errorf("ValidateBody(len=%d): expected error", len(body))
		}
	}
}

func TestValidateTargetRID(t *testing.T) {
	if err := ValidateTargetRID(""); err == nil {
		t.Error("empty: expected error")
	}
	if err := ValidateTargetRID("not-a-rid"); err == nil {
		t.Error("non-RID prefix: expected error")
	}
	if err := ValidateTargetRID("ri.ontology.main.object.abc"); err != nil {
		t.Errorf("valid RID: unexpected %v", err)
	}
}
