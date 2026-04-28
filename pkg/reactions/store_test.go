package reactions

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryStore_CreateIdempotent(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	first := &Reaction{ID: "id-1", UserID: "u-alice", TargetRID: "ri.weave.main.object.42", Emoji: "👍"}
	if err := store.Create(ctx, first); err != nil {
		t.Fatalf("first create: %v", err)
	}
	second := &Reaction{ID: "id-2", UserID: "u-alice", TargetRID: "ri.weave.main.object.42", Emoji: "👍"}
	if err := store.Create(ctx, second); err != nil {
		t.Fatalf("second create: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("idempotent create should return original id, got %q want %q", second.ID, first.ID)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Fatalf("idempotent create should return original timestamp")
	}
}

func TestMemoryStore_DeleteNotFound(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	err := store.Delete(ctx, "u-alice", "ri.weave.main.object.42", "👍")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete on empty: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_DeleteRoundtrip(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	row := &Reaction{ID: "id-1", UserID: "u-alice", TargetRID: "ri.weave.main.object.42", Emoji: "👍"}
	if err := store.Create(ctx, row); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.Delete(ctx, "u-alice", "ri.weave.main.object.42", "👍"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.Delete(ctx, "u-alice", "ri.weave.main.object.42", "👍"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("re-delete: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_AggregateForTarget(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	target := "ri.weave.main.object.42"
	other := "ri.weave.main.object.99"
	rows := []*Reaction{
		{ID: "1", UserID: "u-alice", TargetRID: target, Emoji: "👍"},
		{ID: "2", UserID: "u-bob", TargetRID: target, Emoji: "👍"},
		{ID: "3", UserID: "u-charlie", TargetRID: target, Emoji: "👍"},
		{ID: "4", UserID: "u-alice", TargetRID: target, Emoji: "🚀"},
		{ID: "5", UserID: "u-dan", TargetRID: target, Emoji: "🎉"},
		{ID: "6", UserID: "u-alice", TargetRID: other, Emoji: "👍"},
	}
	for _, row := range rows {
		row.CreatedAt = time.Now().UTC()
		if err := store.Create(ctx, row); err != nil {
			t.Fatalf("create %v: %v", row, err)
		}
	}
	got, err := store.AggregateForTarget(ctx, "u-alice", target)
	if err != nil {
		t.Fatalf("aggregate: %v", err)
	}
	want := []EmojiCount{
		{Emoji: "👍", Count: 3, Mine: true},
		{Emoji: "🎉", Count: 1, Mine: false},
		{Emoji: "🚀", Count: 1, Mine: true},
	}
	if len(got) != len(want) {
		t.Fatalf("aggregate length: got %d want %d (%+v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("aggregate[%d]: got %+v want %+v", i, got[i], want[i])
		}
	}
}

func TestMemoryStore_AggregateEmpty(t *testing.T) {
	store := NewMemoryStore()
	got, err := store.AggregateForTarget(context.Background(), "u-alice", "ri.weave.main.object.42")
	if err != nil {
		t.Fatalf("aggregate empty: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("aggregate empty: want 0 buckets, got %+v", got)
	}
}

func TestValidateEmoji(t *testing.T) {
	cases := []struct {
		name    string
		emoji   string
		wantErr bool
	}{
		{"ok plain", "👍", false},
		{"ok colon-shortcode", ":+1:", false},
		{"ok ascii", "+1", false},
		{"empty", "", true},
		{"all whitespace", " ", true},
		{"leading space", " 👍", true},
		{"trailing space", "👍 ", true},
		{"too long bytes", "12345678901234567890123456789012345", true},
		{"too long runes", "🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉🎉", true},
		{"control char", string([]byte{0x00}), true},
		{"newline", "\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateEmoji(tc.emoji)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateTargetRID(t *testing.T) {
	if err := ValidateTargetRID("ri.weave.main.object.42"); err != nil {
		t.Fatalf("valid RID rejected: %v", err)
	}
	if err := ValidateTargetRID(""); err == nil {
		t.Fatalf("empty RID should error")
	}
	if err := ValidateTargetRID("not-a-rid"); err == nil {
		t.Fatalf("malformed RID should error")
	}
}
