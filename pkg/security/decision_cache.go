package security

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// DecisionCache memoises (userKey, rowKey, ruleSetSig) → allow/deny verdicts
// for the CEL row-level evaluator. Keys are pre-computed uint64 hashes
// emitted by decisionKey() so this struct stays generic and decoupled from
// the rule shape.
//
// **Hot read path:** sync.Map.Load + a single atomic counter increment, no
// mutex acquisition, no time.Now() — so 1M+ hits/sec are sustainable on
// modern hardware (US-432 warm benchmark < 50ms / 1M lookups).
//
// **TTL is lazy.** Entries past their TTL are dropped by ReapExpired (call
// directly or via RunReaperLoop). The maximum staleness window is bounded
// by the reap interval; for hard real-time invalidation, bump the policy
// version, which rotates every cache key referencing the changed bundle.
//
// **LRU is approximate.** When entries exceed Max, an opportunistic sweep
// runs first to drop expired rows; if that's not enough, a full purge
// fires (stateless, O(n)). For the workload this cache serves
// (per-user × per-row decision points), entries naturally roll over as
// rows churn; strict LRU ordering is not load-bearing.
type DecisionCache struct {
	max     int
	ttl     time.Duration
	entries sync.Map // uint64 → *decisionCacheEntry
	size    atomic.Int64

	hits   atomic.Uint64
	misses atomic.Uint64

	clockMu sync.RWMutex
	now     func() time.Time
}

type decisionCacheEntry struct {
	allow    bool
	expireAt time.Time
}

// DecisionCacheStats is a point-in-time snapshot of cache counters suitable
// for metric exposition.
type DecisionCacheStats struct {
	Hits   uint64
	Misses uint64
	Size   int
}

// NewDecisionCache returns a cache with the given capacity and TTL.
// Non-positive max falls back to 65,536 entries; non-positive ttl falls back
// to 5 minutes (matches the PolicyCache default).
func NewDecisionCache(max int, ttl time.Duration) *DecisionCache {
	if max <= 0 {
		max = 1 << 16
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &DecisionCache{max: max, ttl: ttl, now: time.Now}
}

// SetClock injects a custom clock — used by tests to assert TTL semantics
// without wall-clock sleeps. Concurrent-safe.
func (c *DecisionCache) SetClock(now func() time.Time) {
	if now == nil {
		now = time.Now
	}
	c.clockMu.Lock()
	c.now = now
	c.clockMu.Unlock()
}

func (c *DecisionCache) clock() time.Time {
	c.clockMu.RLock()
	now := c.now
	c.clockMu.RUnlock()
	return now()
}

// Get looks up a previously cached verdict. Returns (verdict, true) on hit.
// TTL is enforced lazily by ReapExpired — Get itself never reads the clock.
func (c *DecisionCache) Get(key uint64) (bool, bool) {
	raw, ok := c.entries.Load(key)
	if !ok {
		c.misses.Add(1)
		return false, false
	}
	c.hits.Add(1)
	return raw.(*decisionCacheEntry).allow, true
}

// Put stores a verdict. When the entry count crosses Max, an opportunistic
// sweep drops expired entries first; if the cache is still over Max a full
// purge runs.
func (c *DecisionCache) Put(key uint64, allow bool) {
	entry := &decisionCacheEntry{allow: allow, expireAt: c.clock().Add(c.ttl)}
	if prev, loaded := c.entries.Swap(key, entry); !loaded || prev == nil {
		c.size.Add(1)
	}
	if c.size.Load() > int64(c.max) {
		c.evict()
	}
}

func (c *DecisionCache) evict() {
	now := c.clock()
	c.entries.Range(func(k, v any) bool {
		entry, ok := v.(*decisionCacheEntry)
		if !ok {
			return true
		}
		if !now.Before(entry.expireAt) {
			if c.entries.CompareAndDelete(k, v) {
				c.size.Add(-1)
			}
		}
		return c.size.Load() > int64(c.max)
	})
	if c.size.Load() <= int64(c.max) {
		return
	}
	c.entries.Range(func(k, _ any) bool {
		c.entries.Delete(k)
		c.size.Add(-1)
		return c.size.Load() > 0
	})
}

// Reset drops every entry and zeros the hit/miss counters.
func (c *DecisionCache) Reset() {
	c.entries.Range(func(k, _ any) bool {
		c.entries.Delete(k)
		return true
	})
	c.size.Store(0)
	c.hits.Store(0)
	c.misses.Store(0)
}

// Stats returns a snapshot of cache counters for metric exposition.
func (c *DecisionCache) Stats() DecisionCacheStats {
	return DecisionCacheStats{
		Hits:   c.hits.Load(),
		Misses: c.misses.Load(),
		Size:   int(c.size.Load()),
	}
}

// HitRate returns hits / (hits + misses) in [0, 1]. An empty cache reports 0.
func (c *DecisionCache) HitRate() float64 {
	hits := c.hits.Load()
	misses := c.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// ReapExpired walks the cache and drops entries past their TTL. Returned
// count is the number of entries removed. Safe to call concurrently with
// Get / Put / Reset.
func (c *DecisionCache) ReapExpired() int {
	now := c.clock()
	removed := 0
	c.entries.Range(func(k, v any) bool {
		entry, ok := v.(*decisionCacheEntry)
		if !ok {
			return true
		}
		if !now.Before(entry.expireAt) {
			if c.entries.CompareAndDelete(k, v) {
				c.size.Add(-1)
				removed++
			}
		}
		return true
	})
	return removed
}

// RunReaperLoop runs ReapExpired every interval until ctx is cancelled.
// Pass interval <= 0 to default to TTL/4 with a 30-second floor.
func (c *DecisionCache) RunReaperLoop(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = c.ttl / 4
		if interval < 30*time.Second {
			interval = 30 * time.Second
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.ReapExpired()
		}
	}
}
