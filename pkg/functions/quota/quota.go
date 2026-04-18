// Package quota implements the per-realm, per-minute call limiter backing
// US-218. A bounded Limiter keeps one rolling-window bucket per key and
// returns Allow=false once the caller has exhausted its budget within the
// window — the ExecuteFunction handler maps that into HTTP 429.
package quota

import (
	"sync"
	"time"
)

// DefaultLimit is the production ceiling applied when a call-site does not
// override it. 60 calls / minute is the OSv2 default for interactive
// function invocations; operators tune it via NewLimiter.
const DefaultLimit = 60

// DefaultWindow is the rolling-window width the per-realm quota tracks.
// PRD US-218 mandates "per minute", so this stays at 1 * time.Minute.
const DefaultWindow = time.Minute

// Limiter is a per-key rolling-window rate limiter. It is safe for
// concurrent use. A Limiter with limit=0 or window=0 (zero-value) is a
// pass-through — Allow always returns true so degraded-mode test routers
// that do not wire a real limiter do not silently throttle.
//
// The implementation keeps a bounded FIFO of call timestamps per key.
// Entries older than `window` are evicted lazily on each Allow call, so
// the memory footprint is O(sum of active per-key counts) and expired
// keys stop growing once the window rolls past their last call.
type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string][]time.Time
	nowFunc func() time.Time
}

// NewLimiter returns a Limiter with the given per-key call ceiling and
// rolling-window width. Either zero produces a pass-through Limiter.
func NewLimiter(limit int, window time.Duration) *Limiter {
	return &Limiter{
		limit:   limit,
		window:  window,
		buckets: make(map[string][]time.Time),
		nowFunc: time.Now,
	}
}

// DefaultLimiter returns a Limiter pre-configured with the PRD defaults
// (60 calls / minute, per key).
func DefaultLimiter() *Limiter {
	return NewLimiter(DefaultLimit, DefaultWindow)
}

// SetNowFunc overrides the wall-clock used by Allow. Tests use this to
// exercise the rolling-window eviction path deterministically — the
// convention matches oms.CachedRepository and pkg/oss/computed.
func (l *Limiter) SetNowFunc(fn func() time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if fn == nil {
		l.nowFunc = time.Now
		return
	}
	l.nowFunc = fn
}

// Allow reports whether a call under `key` may proceed right now. It
// evicts any timestamps older than `window` from the key's bucket, then
// either records a new timestamp (and returns true) or rejects the call
// (and returns false) when the bucket is already at its limit.
//
// A nil or zero-value Limiter always allows — guards against misconfigured
// or degraded-mode wiring where no limiter is supplied.
func (l *Limiter) Allow(key string) bool {
	if l == nil || l.limit <= 0 || l.window <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.nowFunc()
	cutoff := now.Add(-l.window)
	bucket := l.buckets[key]

	// Evict expired entries. Timestamps are appended in order, so a
	// single forward scan suffices.
	keep := 0
	for _, t := range bucket {
		if t.After(cutoff) {
			bucket[keep] = t
			keep++
		}
	}
	bucket = bucket[:keep]

	if len(bucket) >= l.limit {
		l.buckets[key] = bucket
		return false
	}

	bucket = append(bucket, now)
	l.buckets[key] = bucket
	return true
}

// Reset discards any recorded calls. Intended for test teardown only.
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buckets = make(map[string][]time.Time)
}
