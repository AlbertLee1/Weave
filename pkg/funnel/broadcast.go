package funnel

import (
	"sync"
	"time"
)

// BroadcastEvent is the in-process payload emitted from the consumer to any
// SSE subscribers after a single edit has been applied to bleve. It is a
// trimmed-down view of funnel.Edit (no auth/user fields, no batch metadata).
type BroadcastEvent struct {
	Type       string                 `json:"type"` // CREATE | MODIFY | DELETE
	ObjectType string                 `json:"objectType"`
	PrimaryKey string                 `json:"primaryKey"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	EditedAt   time.Time              `json:"editedAt"`
}

// Broadcast is an in-memory fan-out hub. The funnel consumer calls Publish
// after each successful edit application; HTTP handlers (the SSE endpoint)
// call Subscribe to receive the live event stream and Unsubscribe on
// disconnect.
//
// Slow subscribers do not block the consumer: when a subscriber's buffered
// channel is full the event is dropped on the floor for that subscriber
// while other subscribers continue to receive it. This is intentional and
// matches the SSE design — clients that fall behind miss events rather than
// stalling the write pipeline.
type Broadcast struct {
	mu   sync.RWMutex
	subs map[int64]chan BroadcastEvent
	next int64
}

// NewBroadcast constructs an empty broadcast hub.
func NewBroadcast() *Broadcast {
	return &Broadcast{
		subs: make(map[int64]chan BroadcastEvent),
	}
}

// Subscribe registers a new subscriber and returns its id together with a
// receive-only channel buffered to the caller-supplied size. The caller must
// invoke Unsubscribe(id) when finished — typically via defer in an HTTP
// handler — so the channel is closed and removed from the hub.
//
// A buffer size <= 0 is clamped to 1 to ensure Publish always has at least
// one slot to attempt before dropping.
func (b *Broadcast) Subscribe(buffer int) (int64, <-chan BroadcastEvent) {
	if buffer <= 0 {
		buffer = 1
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	id := b.next
	ch := make(chan BroadcastEvent, buffer)
	b.subs[id] = ch
	return id, ch
}

// Unsubscribe removes the subscription identified by id, closing its channel.
// Calling Unsubscribe with an unknown id (or twice with the same id) is a
// safe no-op so handlers can defer it without checking state.
func (b *Broadcast) Unsubscribe(id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch, ok := b.subs[id]
	if !ok {
		return
	}
	delete(b.subs, id)
	close(ch)
}

// Publish delivers the event to every current subscriber. Each delivery is
// non-blocking: a full channel causes the event to be dropped for that
// subscriber. Publish never blocks the calling goroutine (the funnel
// consumer) regardless of subscriber state.
func (b *Broadcast) Publish(event BroadcastEvent) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
			// Slow subscriber — drop event for this channel only.
		}
	}
}
