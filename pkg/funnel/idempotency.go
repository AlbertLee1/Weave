package funnel

import (
	"sync"
	"time"
)

// defaultIdempotencyMaxSize bounds the in-memory dedupe cache so a long-lived
// consumer cannot grow unbounded on adversarial inputs. The exact figure is
// not load-bearing — it is sized to comfortably cover the in-flight batch
// window for the single-machine deployment Weave targets.
const defaultIdempotencyMaxSize = 1024

// idempotencyCache tracks recently-applied batch IDs so the consumer can
// short-circuit duplicate deliveries inside a sliding window. window<=0 (the
// zero value) disables the cache and seenAndStamp always reports "fresh".
type idempotencyCache struct {
	mu      sync.Mutex
	seen    map[string]time.Time
	window  time.Duration
	maxSize int
}

// setWindow updates the dedup window. A non-positive value disables the cache
// and is the documented "off" switch — callers can flip it on by passing a
// positive duration. Subsequent seenAndStamp calls observe the new window
// immediately. setWindow also initialises the per-cache bound on first use so
// SetIdempotencyWindow callers don't have to set both.
func (i *idempotencyCache) setWindow(d time.Duration) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.window = d
	if i.maxSize == 0 {
		i.maxSize = defaultIdempotencyMaxSize
	}
}

// seenAndStamp returns true if id was recorded within the configured window
// and is therefore a duplicate; otherwise it stamps id with now and returns
// false. Disabled caches (window<=0) and empty IDs are never considered
// duplicates so the call is safe to drop into the apply path unconditionally.
//
// On overflow the oldest stamped entry is evicted so the cache stays bounded.
// Eviction iterates the map and picks the lowest timestamp — adequate for the
// expected cardinality (≤ 1024 default) and avoids dragging in a heap.
func (i *idempotencyCache) seenAndStamp(id string, now time.Time) bool {
	if id == "" {
		return false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.window <= 0 {
		return false
	}
	if i.seen == nil {
		i.seen = make(map[string]time.Time)
	}

	if t, ok := i.seen[id]; ok {
		if now.Sub(t) <= i.window {
			return true
		}
		// Stale entry — fall through and refresh below.
		delete(i.seen, id)
	}

	// Opportunistic sweep so a low-traffic cache eventually shrinks rather
	// than holding onto every expired stamp until overflow forces eviction.
	for k, t := range i.seen {
		if now.Sub(t) > i.window {
			delete(i.seen, k)
		}
	}

	if i.maxSize > 0 && len(i.seen) >= i.maxSize {
		var oldestKey string
		var oldestTime time.Time
		first := true
		for k, t := range i.seen {
			if first || t.Before(oldestTime) {
				oldestKey, oldestTime = k, t
				first = false
			}
		}
		delete(i.seen, oldestKey)
	}

	i.seen[id] = now
	return false
}

// size returns the current number of tracked entries. Exposed for tests.
func (i *idempotencyCache) size() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.seen)
}
