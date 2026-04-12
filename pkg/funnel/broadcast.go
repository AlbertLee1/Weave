package funnel

import (
	"sync"
	"time"
)

// BroadcastEvent is the in-process payload emitted from the consumer to any
// SSE subscribers after a single edit has been applied to bleve. It is a
// trimmed-down view of funnel.Edit (no auth/user fields, no batch metadata).
//
// Sequence carries the originating NATS stream sequence number so SSE
// clients can use it as the Server-Sent Events `id:` value and reconnect
// with a Last-Event-ID header (US-057). Zero means "no sequence available"
// (in-process callers that bypass NATS) and the replay ring buffer skips
// storing those events.
type BroadcastEvent struct {
	Type       string                 `json:"type"` // CREATE | MODIFY | DELETE
	ObjectType string                 `json:"objectType"`
	PrimaryKey string                 `json:"primaryKey"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	EditedAt   time.Time              `json:"editedAt"`
	Sequence   uint64                 `json:"sequence,omitempty"`
}

// DefaultBroadcastReplayCapacity is the default size of the ring buffer used
// for SSE Last-Event-ID replay (US-057). Tuned so that a brief client
// disconnect (say, a few seconds of network flap) has room for any edits
// applied in the interim without growing memory unboundedly. Operators can
// override via NewBroadcastWithReplay.
const DefaultBroadcastReplayCapacity = 1024

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
//
// US-057: Broadcast additionally maintains a bounded ring buffer of the
// most recent events keyed by Sequence. SubscribeWithReplay uses it to
// atomically snapshot any events newer than a client-supplied Last-Event-ID
// under the same lock that registers the new live subscription, so no
// events fall between the replay tail and the live head.
type Broadcast struct {
	mu   sync.Mutex
	subs map[int64]chan BroadcastEvent
	next int64

	ring       []BroadcastEvent
	ringMax    int
	lastSeqSet bool
	lastSeq    uint64
}

// NewBroadcast constructs an empty broadcast hub with the default replay
// ring capacity (DefaultBroadcastReplayCapacity).
func NewBroadcast() *Broadcast {
	return NewBroadcastWithReplay(DefaultBroadcastReplayCapacity)
}

// NewBroadcastWithReplay constructs a hub with a custom ring buffer size.
// A size <= 0 disables replay entirely — Publish still fans out to live
// subscribers but SubscribeWithReplay yields no historical events.
func NewBroadcastWithReplay(ringCapacity int) *Broadcast {
	if ringCapacity < 0 {
		ringCapacity = 0
	}
	return &Broadcast{
		subs:    make(map[int64]chan BroadcastEvent),
		ringMax: ringCapacity,
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
	return b.SubscribeWithReplay(buffer, 0)
}

// SubscribeWithReplay is the US-057 entry point. It behaves like Subscribe
// but additionally replays every buffered event whose Sequence is strictly
// greater than fromSeq into the new subscriber's channel BEFORE the live
// subscription begins producing. The replay + live subscription are
// performed under a single hub lock so no event can slip between the ring
// snapshot and the live fan-out — any event published after the call
// returns is delivered through the live path exactly once.
//
// fromSeq == 0 means "no prior cursor" — all buffered events are replayed.
// A fromSeq larger than every buffered Sequence yields no replay. The
// returned channel capacity is sized to max(buffer, replayCount+buffer) so
// the pre-loaded replay events never block the caller thread.
func (b *Broadcast) SubscribeWithReplay(buffer int, fromSeq uint64) (int64, <-chan BroadcastEvent) {
	if buffer <= 0 {
		buffer = 1
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	var replay []BroadcastEvent
	for _, e := range b.ring {
		if e.Sequence > fromSeq {
			replay = append(replay, e)
		}
	}

	chCap := buffer
	if need := len(replay) + buffer; need > chCap {
		chCap = need
	}
	ch := make(chan BroadcastEvent, chCap)
	for _, e := range replay {
		ch <- e
	}

	b.next++
	id := b.next
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

// Publish delivers the event to every current subscriber and, when Sequence
// is non-zero and the hub has replay capacity, records it into the bounded
// ring buffer for future Last-Event-ID replay. Each delivery is non-blocking:
// a full channel causes the event to be dropped for that subscriber. Publish
// never blocks the calling goroutine (the funnel consumer) regardless of
// subscriber state.
//
// Out-of-order sequences (event.Sequence <= lastSeen) are NOT stored in the
// ring — the NATS consumer forwards events in stream order, so an
// out-of-order arrival indicates either an in-process test helper that does
// not simulate sequence or a bug upstream; storing would risk serving stale
// tail events during replay.
func (b *Broadcast) Publish(event BroadcastEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ringMax > 0 && event.Sequence > 0 {
		if !b.lastSeqSet || event.Sequence > b.lastSeq {
			b.ring = append(b.ring, event)
			if len(b.ring) > b.ringMax {
				b.ring = b.ring[len(b.ring)-b.ringMax:]
			}
			b.lastSeq = event.Sequence
			b.lastSeqSet = true
		}
	}
	for _, ch := range b.subs {
		select {
		case ch <- event:
		default:
			// Slow subscriber — drop event for this channel only.
		}
	}
}
