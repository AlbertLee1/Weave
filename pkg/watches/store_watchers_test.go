package watches

import (
	"context"
	"testing"
)

// TestMemoryStore_WatchersFor exercises the fan-out lookup that the
// US-338 activity-notification consumer uses to ask "who is watching
// this target_rid?". The shape is map[targetRID][]userID; targets with
// no watchers are absent from the map (callers iterate).
func TestMemoryStore_WatchersFor(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	// Three watchers across two targets:
	//   alice + bob → object.42
	//   carol      → object.7
	if err := s.Create(ctx, &Watch{ID: "w1", UserID: "user:alice", TargetRID: "ri.weave.main.object.42"}); err != nil {
		t.Fatalf("create alice/42: %v", err)
	}
	if err := s.Create(ctx, &Watch{ID: "w2", UserID: "user:bob", TargetRID: "ri.weave.main.object.42"}); err != nil {
		t.Fatalf("create bob/42: %v", err)
	}
	if err := s.Create(ctx, &Watch{ID: "w3", UserID: "user:carol", TargetRID: "ri.weave.main.object.7"}); err != nil {
		t.Fatalf("create carol/7: %v", err)
	}

	got, err := s.WatchersFor(ctx, []string{
		"ri.weave.main.object.42",
		"ri.weave.main.object.7",
		"ri.weave.main.object.99", // unwatched
	})
	if err != nil {
		t.Fatalf("WatchersFor: %v", err)
	}

	// Unwatched target should be absent (or empty).
	if list, ok := got["ri.weave.main.object.99"]; ok && len(list) != 0 {
		t.Fatalf("unwatched target leaked watchers: %v", list)
	}

	if got["ri.weave.main.object.7"][0] != "user:carol" {
		t.Fatalf("carol should watch /7, got %v", got["ri.weave.main.object.7"])
	}

	target42 := got["ri.weave.main.object.42"]
	if len(target42) != 2 {
		t.Fatalf("expected 2 watchers for /42, got %v", target42)
	}
	seen := map[string]bool{}
	for _, id := range target42 {
		seen[id] = true
	}
	if !seen["user:alice"] || !seen["user:bob"] {
		t.Fatalf("expected alice + bob, got %v", target42)
	}
}

// TestMemoryStore_WatchersFor_Empty ensures an empty/nil input returns
// an empty map so the activity consumer can short-circuit cheaply.
func TestMemoryStore_WatchersFor_Empty(t *testing.T) {
	s := NewMemoryStore()
	for _, in := range [][]string{nil, {}} {
		got, err := s.WatchersFor(context.Background(), in)
		if err != nil {
			t.Fatalf("WatchersFor(%v): %v", in, err)
		}
		if len(got) != 0 {
			t.Fatalf("WatchersFor(%v) want empty, got %v", in, got)
		}
	}
}
