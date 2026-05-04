// Package cache implements the bounded LRU+TTL result cache backing US-221
// (Function 结果缓存). When a Function is flagged `pure=true` the
// ExecuteFunction handler keys this cache on `rid@version + hash(params)`
// and returns the cached value when the hash matches a fresh entry.
//
// The cache is intentionally process-local: invalidation across a cluster is
// out of scope here, and the 5-minute TTL caps how long a stale value can
// linger after the function source is updated.
package cache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultCapacity is the maximum number of entries retained by a Cache built
// via NewDefault. PRD US-221 calls for "LRU 缓存 10k entry"; operators that
// need a different ceiling instantiate via NewCache directly.
const DefaultCapacity = 10_000

// DefaultTTL is the per-entry freshness window applied by NewDefault. PRD
// US-221 mandates "TTL 5 分钟". An entry whose age exceeds the TTL is
// treated as a miss and the prior value is evicted on access.
const DefaultTTL = 5 * time.Minute

// Cache is a bounded, in-memory LRU map of Function execution results
// keyed by an opaque string. It is safe for concurrent use.
//
// Behaviour:
//   - Capacity caps the entry count. Inserting beyond the cap evicts the
//     least-recently-used entry.
//   - TTL caps the entry age. Get returns (nil, false) when the entry is
//     present but expired, and removes the stale entry as a side effect.
//   - A Cache with capacity<=0 or ttl<=0 (zero-value) is a pass-through —
//     Get always misses, Put silently discards, so callers that forget to
//     wire a cache do not silently change the function's semantics.
type Cache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	ll       *list.List
	entries  map[string]*list.Element
	nowFunc  func() time.Time
}

type cacheEntry struct {
	key       string
	value     interface{}
	expiresAt time.Time
}

// NewCache returns a Cache with the given LRU capacity and per-entry TTL.
// Either zero produces a pass-through Cache (Get always misses, Put no-ops),
// matching the degraded-mode contract every other oms-side optional hook
// follows.
func NewCache(capacity int, ttl time.Duration) *Cache {
	c := &Cache{
		capacity: capacity,
		ttl:      ttl,
		nowFunc:  time.Now,
	}
	if capacity > 0 {
		c.ll = list.New()
		c.entries = make(map[string]*list.Element, capacity)
	}
	return c
}

// NewDefault returns a Cache pre-configured with the PRD defaults
// (10k entries, 5-minute TTL).
func NewDefault() *Cache {
	return NewCache(DefaultCapacity, DefaultTTL)
}

// SetNowFunc overrides the wall-clock used for TTL accounting. Tests inject
// a deterministic clock to exercise the expiry path without time.Sleep —
// same convention as pkg/functions/quota.Limiter and
// pkg/oss/computed.Resolver.
func (c *Cache) SetNowFunc(fn func() time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if fn == nil {
		c.nowFunc = time.Now
		return
	}
	c.nowFunc = fn
}

// Get returns the cached value for key when present and not expired. The
// access promotes the entry to most-recently-used. On a miss (absent or
// expired) Get returns (nil, false) and the stale entry is removed.
//
// A nil or pass-through (capacity<=0 / ttl<=0) Cache always misses.
func (c *Cache) Get(key string) (interface{}, bool) {
	if c == nil || c.capacity <= 0 || c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	entry := elem.Value.(*cacheEntry)
	if !c.nowFunc().Before(entry.expiresAt) {
		c.removeElement(elem)
		return nil, false
	}
	c.ll.MoveToFront(elem)
	return entry.value, true
}

// Put writes value under key with a fresh expiry of now+ttl. When the cache
// is at capacity Put evicts the least-recently-used entry first. A repeat
// Put on the same key refreshes the value AND resets the TTL window.
//
// A nil or pass-through Cache silently discards.
func (c *Cache) Put(key string, value interface{}) {
	if c == nil || c.capacity <= 0 || c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.nowFunc()
	expiresAt := now.Add(c.ttl)

	if elem, ok := c.entries[key]; ok {
		entry := elem.Value.(*cacheEntry)
		entry.value = value
		entry.expiresAt = expiresAt
		c.ll.MoveToFront(elem)
		return
	}

	entry := &cacheEntry{key: key, value: value, expiresAt: expiresAt}
	elem := c.ll.PushFront(entry)
	c.entries[key] = elem

	if c.ll.Len() > c.capacity {
		c.removeElement(c.ll.Back())
	}
}

// Len reports the current entry count (including entries whose TTL has
// expired but not yet been observed via Get).
func (c *Cache) Len() int {
	if c == nil || c.capacity <= 0 || c.ttl <= 0 {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}

// Invalidate drops the entry for key if present. Returns true when an
// entry was removed.
func (c *Cache) Invalidate(key string) bool {
	if c == nil || c.capacity <= 0 || c.ttl <= 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	elem, ok := c.entries[key]
	if !ok {
		return false
	}
	c.removeElement(elem)
	return true
}

// InvalidatePrefix drops every entry whose key starts with prefix and
// returns the number of entries removed (US-425). Used to flush all cached
// results for a Function after a publish or after an upstream object
// change — the cache key shape `<rid>@<version>#<hash>` makes a prefix
// match on `<rid>@` (or `<rid>@<version>#`) the natural way to scope the
// flush. An empty prefix is a no-op (returns 0) so a misconfigured caller
// cannot accidentally clear the entire cache.
//
// A nil or pass-through Cache is a no-op (returns 0).
func (c *Cache) InvalidatePrefix(prefix string) int {
	if c == nil || c.capacity <= 0 || c.ttl <= 0 || prefix == "" {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	removed := 0
	for k, elem := range c.entries {
		if strings.HasPrefix(k, prefix) {
			c.removeElement(elem)
			removed++
		}
	}
	return removed
}

// Reset discards every entry. Intended for test teardown.
func (c *Cache) Reset() {
	if c == nil || c.capacity <= 0 || c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ll.Init()
	c.entries = make(map[string]*list.Element, c.capacity)
}

func (c *Cache) removeElement(elem *list.Element) {
	entry := elem.Value.(*cacheEntry)
	delete(c.entries, entry.key)
	c.ll.Remove(elem)
}

// Key builds the canonical cache key for a Function call: `rid@version`
// followed by a SHA-256 hex digest of the canonically-serialised params.
// Param maps with the same logical content always hash to the same digest
// because keys are sorted before encoding — `{"a":1,"b":2}` and
// `{"b":2,"a":1}` collapse to one bucket.
//
// nil / empty params hash to the SHA-256 of `{}` so the absence-of-params
// case still produces a deterministic key.
func Key(rid, version string, params map[string]interface{}) string {
	hash := hashParams(params)
	if version == "" {
		return rid + "@" + hash
	}
	return rid + "@" + version + "#" + hash
}

func hashParams(params map[string]interface{}) string {
	if len(params) == 0 {
		sum := sha256.Sum256([]byte("{}"))
		return hex.EncodeToString(sum[:])
	}
	canonical, err := canonicalJSON(params)
	if err != nil {
		// canonicalJSON only fails when a value is not JSON-encodable; fall
		// back to a fmt-rendered form so the key stays deterministic for
		// the duration of the process. The cached value remains correct
		// because Get and Put share the same fallback.
		canonical = []byte(fmt.Sprintf("%v", params))
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// canonicalJSON encodes v with object keys sorted recursively so that two
// logically-equal maps marshal to the same bytes.
func canonicalJSON(v interface{}) ([]byte, error) {
	switch typed := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for k := range typed {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]json.RawMessage, 0, len(keys))
		for _, k := range keys {
			child, err := canonicalJSON(typed[k])
			if err != nil {
				return nil, err
			}
			keyBytes, err := json.Marshal(k)
			if err != nil {
				return nil, err
			}
			parts = append(parts, append(append(keyBytes, ':'), child...))
		}
		out := []byte{'{'}
		for i, p := range parts {
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, p...)
		}
		out = append(out, '}')
		return out, nil
	case []interface{}:
		parts := make([]json.RawMessage, 0, len(typed))
		for _, item := range typed {
			child, err := canonicalJSON(item)
			if err != nil {
				return nil, err
			}
			parts = append(parts, child)
		}
		out := []byte{'['}
		for i, p := range parts {
			if i > 0 {
				out = append(out, ',')
			}
			out = append(out, p...)
		}
		out = append(out, ']')
		return out, nil
	default:
		return json.Marshal(typed)
	}
}
