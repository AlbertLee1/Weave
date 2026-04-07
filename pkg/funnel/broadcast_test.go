package funnel

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestBroadcast_Publish_FanOut_ToAllSubscribers verifies that an event
// published to the broadcast hub is delivered to every subscribed channel.
func TestBroadcast_Publish_FanOut_ToAllSubscribers(t *testing.T) {
	b := NewBroadcast()

	const numSubs = 5
	ids := make([]int64, numSubs)
	chans := make([]<-chan BroadcastEvent, numSubs)
	for i := 0; i < numSubs; i++ {
		ids[i], chans[i] = b.Subscribe(8)
	}
	t.Cleanup(func() {
		for _, id := range ids {
			b.Unsubscribe(id)
		}
	})

	event := BroadcastEvent{
		Type:       "CREATE",
		ObjectType: "employee",
		PrimaryKey: "emp-1",
		Properties: map[string]interface{}{"name": "Alice"},
		EditedAt:   time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC),
	}
	b.Publish(event)

	for i, ch := range chans {
		select {
		case got := <-ch:
			if got.Type != event.Type {
				t.Errorf("sub %d: expected Type %q, got %q", i, event.Type, got.Type)
			}
			if got.ObjectType != event.ObjectType {
				t.Errorf("sub %d: expected ObjectType %q, got %q", i, event.ObjectType, got.ObjectType)
			}
			if got.PrimaryKey != event.PrimaryKey {
				t.Errorf("sub %d: expected PrimaryKey %q, got %q", i, event.PrimaryKey, got.PrimaryKey)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("sub %d: did not receive event", i)
		}
	}
}

// TestBroadcast_Unsubscribe_DoesNotReceive verifies that after Unsubscribe a
// subscriber's channel is closed and no further events are delivered.
func TestBroadcast_Unsubscribe_DoesNotReceive(t *testing.T) {
	b := NewBroadcast()

	id, ch := b.Subscribe(4)

	// First publish should deliver.
	b.Publish(BroadcastEvent{Type: "CREATE", ObjectType: "x", PrimaryKey: "1"})
	select {
	case <-ch:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected first event before unsubscribe")
	}

	b.Unsubscribe(id)

	// After Unsubscribe the channel should be closed and the next read should
	// either return zero-value (closed) or, if events were drained, time out
	// quickly. We must NOT receive a second published event.
	b.Publish(BroadcastEvent{Type: "CREATE", ObjectType: "x", PrimaryKey: "2"})

	select {
	case ev, ok := <-ch:
		if ok {
			t.Fatalf("did not expect event after unsubscribe, got %+v", ev)
		}
		// closed channel returning zero-value is acceptable
	case <-time.After(50 * time.Millisecond):
		// no message delivered — also acceptable
	}
}

// TestBroadcast_SlowSubscriber_DropsEvents ensures that a slow subscriber
// (whose buffered channel is full) does not block the publisher. Events are
// dropped on the floor for that subscriber while other subscribers still see
// them.
func TestBroadcast_SlowSubscriber_DropsEvents(t *testing.T) {
	b := NewBroadcast()

	// Slow subscriber: buffer size 1, never drains.
	slowID, slowCh := b.Subscribe(1)
	t.Cleanup(func() { b.Unsubscribe(slowID) })

	// Fast subscriber to confirm publisher kept moving.
	fastID, fastCh := b.Subscribe(8)
	t.Cleanup(func() { b.Unsubscribe(fastID) })

	// Publish 3 events. The slow subscriber will only receive 1 (the first,
	// which fills its buffer); the rest must be dropped without blocking.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 3; i++ {
			b.Publish(BroadcastEvent{
				Type:       "CREATE",
				ObjectType: "x",
				PrimaryKey: "k",
			})
		}
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publisher blocked on slow subscriber")
	}

	// Slow subscriber should have exactly one buffered event.
	select {
	case <-slowCh:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("slow subscriber missed first event")
	}

	// Should not have a second event waiting (it was dropped).
	select {
	case ev := <-slowCh:
		t.Fatalf("slow subscriber received unexpected second event: %+v", ev)
	case <-time.After(20 * time.Millisecond):
		// expected: nothing waiting
	}

	// Fast subscriber should have received all 3.
	for i := 0; i < 3; i++ {
		select {
		case <-fastCh:
		case <-time.After(500 * time.Millisecond):
			t.Fatalf("fast subscriber missed event %d", i)
		}
	}
}

// TestBroadcast_Concurrent_SubscribeUnsubscribe stresses the hub with many
// goroutines subscribing, publishing, and unsubscribing concurrently. Run
// with -race to verify no data race exists.
func TestBroadcast_Concurrent_SubscribeUnsubscribe(t *testing.T) {
	b := NewBroadcast()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Publisher goroutines
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					b.Publish(BroadcastEvent{
						Type:       "CREATE",
						ObjectType: "x",
						PrimaryKey: "k",
					})
				}
			}
		}()
	}

	// Subscribe/unsubscribe churn
	var totalSubs atomic.Int64
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					id, ch := b.Subscribe(4)
					totalSubs.Add(1)
					// drain a few then unsubscribe
					for j := 0; j < 3; j++ {
						select {
						case <-ch:
						case <-time.After(5 * time.Millisecond):
						}
					}
					b.Unsubscribe(id)
				}
			}
		}()
	}

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()

	if totalSubs.Load() == 0 {
		t.Fatal("expected at least one subscribe")
	}
}

// TestBroadcast_Unsubscribe_Idempotent verifies that calling Unsubscribe with
// an unknown id (or twice) does not panic.
func TestBroadcast_Unsubscribe_Idempotent(t *testing.T) {
	b := NewBroadcast()
	id, _ := b.Subscribe(1)
	b.Unsubscribe(id)
	b.Unsubscribe(id)        // double-unsubscribe
	b.Unsubscribe(9999)      // never registered
}

// TestBroadcast_Publish_NoSubscribers verifies that Publish with no
// subscribers is a safe no-op.
func TestBroadcast_Publish_NoSubscribers(t *testing.T) {
	b := NewBroadcast()
	// must not panic or block
	b.Publish(BroadcastEvent{Type: "CREATE", ObjectType: "x", PrimaryKey: "1"})
}
