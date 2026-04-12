package security

import (
	"container/list"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2/search/query"
)

// PolicyCache is an in-memory LRU + TTL cache for compiled policy queries.
// Entries are keyed by (userID, objectTypeRID, policyVersion) so a policy
// update that bumps its version automatically short-circuits any stale hit,
// while the LRU + TTL bounds keep memory use bounded in steady state.
//
// The zero value is not usable — call NewPolicyCache.
type PolicyCache struct {
	mu      sync.Mutex
	max     int
	ttl     time.Duration
	entries map[policyCacheKey]*list.Element
	lru     *list.List
	hits    uint64
	misses  uint64
	now     func() time.Time
}

type policyCacheKey struct {
	userID        string
	objectTypeRID string
	version       int64
}

type policyCacheEntry struct {
	key      policyCacheKey
	query    query.Query
	expireAt time.Time
}

// PolicyCacheStats is a point-in-time snapshot of cache counters.
type PolicyCacheStats struct {
	Hits   uint64
	Misses uint64
	Size   int
}

// NewPolicyCache returns a cache with the given capacity and TTL.
// Non-positive max falls back to 1024 entries; non-positive ttl falls back
// to 5 minutes, which matches the US-045 spec.
func NewPolicyCache(max int, ttl time.Duration) *PolicyCache {
	if max <= 0 {
		max = 1024
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &PolicyCache{
		max:     max,
		ttl:     ttl,
		entries: make(map[policyCacheKey]*list.Element),
		lru:     list.New(),
		now:     time.Now,
	}
}

// Get looks up a previously compiled query for this triple. Misses and
// TTL-expired hits both count as misses for hit-rate purposes.
func (c *PolicyCache) Get(userID, objectTypeRID string, version int64) (query.Query, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := policyCacheKey{userID: userID, objectTypeRID: objectTypeRID, version: version}
	el, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}
	entry := el.Value.(*policyCacheEntry)
	if !c.now().Before(entry.expireAt) {
		c.lru.Remove(el)
		delete(c.entries, key)
		c.misses++
		return nil, false
	}
	c.lru.MoveToFront(el)
	c.hits++
	return entry.query, true
}

// Put stores a compiled query, evicting the least-recently used entry when
// the cache is already at capacity. Refreshing an existing key resets its
// TTL.
func (c *PolicyCache) Put(userID, objectTypeRID string, version int64, q query.Query) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := policyCacheKey{userID: userID, objectTypeRID: objectTypeRID, version: version}
	if el, ok := c.entries[key]; ok {
		entry := el.Value.(*policyCacheEntry)
		entry.query = q
		entry.expireAt = c.now().Add(c.ttl)
		c.lru.MoveToFront(el)
		return
	}
	entry := &policyCacheEntry{
		key:      key,
		query:    q,
		expireAt: c.now().Add(c.ttl),
	}
	el := c.lru.PushFront(entry)
	c.entries[key] = el
	for c.lru.Len() > c.max {
		oldest := c.lru.Back()
		if oldest == nil {
			break
		}
		c.lru.Remove(oldest)
		delete(c.entries, oldest.Value.(*policyCacheEntry).key)
	}
}

// InvalidateObjectType drops every entry whose key references the given
// ObjectType RID. Called from Engine.SetPolicies so version-stale entries
// don't sit around taking space until TTL.
func (c *PolicyCache) InvalidateObjectType(objectTypeRID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, el := range c.entries {
		if key.objectTypeRID == objectTypeRID {
			c.lru.Remove(el)
			delete(c.entries, key)
		}
	}
}

// Stats returns a snapshot of cache counters for metric exposition.
func (c *PolicyCache) Stats() PolicyCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return PolicyCacheStats{
		Hits:   c.hits,
		Misses: c.misses,
		Size:   c.lru.Len(),
	}
}

// HitRate returns hits / (hits + misses) in [0, 1]. An empty cache reports 0.
func (c *PolicyCache) HitRate() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	total := c.hits + c.misses
	if total == 0 {
		return 0
	}
	return float64(c.hits) / float64(total)
}
