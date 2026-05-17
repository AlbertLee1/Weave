package funnel

import (
	"testing"
	"time"
)

// TestBroadcast_WindowPrune_DropsEventsOlderThanWindow exercises the US-459
// time-bounded replay window. Events whose EditedAt falls outside the
// configured window MUST be evicted from the ring at the next Publish, so a
// fresh subscriber with a cursor inside the retained tail still receives a
// gap-free replay — the eviction never silently truncates the visible head.
func TestBroadcast_WindowPrune_DropsEventsOlderThanWindow(t *testing.T) {
	b := NewBroadcastWithWindow(1 * time.Minute)

	// Stale event: EditedAt 10 minutes ago → outside the 1-minute window.
	// Published first so it ends up at the ring head before eviction.
	b.Publish(BroadcastEvent{
		Type: "CREATE", ObjectType: "order", PrimaryKey: "old", Sequence: 10,
		EditedAt: time.Now().Add(-10 * time.Minute),
	})
	// Two fresh events that should survive the window prune. Sequencing
	// matters: the test asserts that a cursor at the OLDER fresh seq still
	// resumes onto the NEWER fresh seq without 410.
	b.Publish(BroadcastEvent{
		Type: "CREATE", ObjectType: "order", PrimaryKey: "mid", Sequence: 20,
		EditedAt: time.Now(),
	})
	b.Publish(BroadcastEvent{
		Type: "CREATE", ObjectType: "order", PrimaryKey: "new", Sequence: 30,
		EditedAt: time.Now(),
	})

	// Cursor at seq=20 (the older fresh event). Hub should:
	//   - not flag out-of-window (the ring head is seq 20, fromSeq+1=21 < 30
	//     would mean "events evicted between" but the ring contains 20 itself).
	//   - replay seq 30 only (everything strictly greater than the cursor).
	_, ch, outOfWindow := b.SubscribeWithReplayWindow(8, 20)
	if outOfWindow {
		t.Fatalf("outOfWindow = true, want false for cursor matching oldest retained event")
	}
	select {
	case got := <-ch:
		if got.Sequence != 30 {
			t.Errorf("first replay seq = %d, want 30", got.Sequence)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected replay of seq 30")
	}
	select {
	case extra := <-ch:
		t.Errorf("unexpected extra replay event: seq=%d", extra.Sequence)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestBroadcast_OutOfWindow_Signaled_WhenCursorPredatesRing covers the
// "cursor 越界" criterion: a client whose cursor falls behind the oldest event
// the hub still retains is signaled out-of-window so the SSE handler can
// emit a 410 Gone instead of silently skipping data.
func TestBroadcast_OutOfWindow_Signaled_WhenCursorPredatesRing(t *testing.T) {
	b := NewBroadcastWithWindow(1 * time.Minute)

	// Both events are stale → pruned. lastSeq still records 5 so the hub knows
	// it has produced events the client may have seen.
	b.Publish(BroadcastEvent{
		Type: "CREATE", ObjectType: "order", PrimaryKey: "a", Sequence: 1,
		EditedAt: time.Now().Add(-10 * time.Minute),
	})
	b.Publish(BroadcastEvent{
		Type: "CREATE", ObjectType: "order", PrimaryKey: "b", Sequence: 5,
		EditedAt: time.Now().Add(-10 * time.Minute),
	})

	_, _, outOfWindow := b.SubscribeWithReplayWindow(8, 2)
	if !outOfWindow {
		t.Fatal("outOfWindow = false, want true for cursor older than evicted tail")
	}
}

// TestBroadcast_OutOfWindow_NotSignaled_WhenCursorAhead preserves the
// US-057 / TestSSEReplayRingBufferSkipsSeenEvents semantics: a cursor that is
// AHEAD of lastSeq (server has never produced an event with that seq) does
// NOT trigger 410 Gone — it just yields zero replay and waits for the next
// live event.
func TestBroadcast_OutOfWindow_NotSignaled_WhenCursorAhead(t *testing.T) {
	b := NewBroadcastWithWindow(1 * time.Minute)
	b.Publish(BroadcastEvent{
		Type: "CREATE", ObjectType: "order", PrimaryKey: "a", Sequence: 5,
		EditedAt: time.Now(),
	})

	_, ch, outOfWindow := b.SubscribeWithReplayWindow(4, 100)
	if outOfWindow {
		t.Fatal("outOfWindow = true, want false when cursor > lastSeq (client is ahead)")
	}
	// No replay expected.
	select {
	case ev := <-ch:
		t.Fatalf("unexpected replay event: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestBroadcast_OutOfWindow_NotSignaled_WhenZeroCursor preserves the
// "fresh client" semantics: fromSeq=0 means "no prior cursor" and should
// always succeed with whatever the hub retains, never 410 Gone.
func TestBroadcast_OutOfWindow_NotSignaled_WhenZeroCursor(t *testing.T) {
	b := NewBroadcastWithWindow(1 * time.Minute)
	b.Publish(BroadcastEvent{
		Type: "CREATE", ObjectType: "order", PrimaryKey: "a", Sequence: 1,
		EditedAt: time.Now().Add(-10 * time.Minute), // stale, will be pruned
	})

	_, _, outOfWindow := b.SubscribeWithReplayWindow(4, 0)
	if outOfWindow {
		t.Fatal("outOfWindow = true, want false for fromSeq=0 (no prior cursor)")
	}
}
