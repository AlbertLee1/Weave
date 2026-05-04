package cache_test

import (
	"sync"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/functions/cache"
)

func TestCache_HitAndMiss(t *testing.T) {
	c := cache.NewCache(8, time.Minute)
	if _, ok := c.Get("k1"); ok {
		t.Fatalf("empty cache must miss")
	}
	c.Put("k1", "v1")
	got, ok := c.Get("k1")
	if !ok || got != "v1" {
		t.Fatalf("expected hit (v1), got (%v, %v)", got, ok)
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	c := cache.NewCache(8, 5*time.Minute)
	now := time.Unix(1_700_000_000, 0)
	c.SetNowFunc(func() time.Time { return now })

	c.Put("k1", "v1")
	if _, ok := c.Get("k1"); !ok {
		t.Fatalf("expected hit immediately after Put")
	}

	// Advance just under the TTL — still a hit.
	now = now.Add(4*time.Minute + 59*time.Second)
	if _, ok := c.Get("k1"); !ok {
		t.Fatalf("expected hit just before TTL boundary")
	}

	// Advance past the TTL — now a miss, and the entry is evicted.
	now = now.Add(2 * time.Second)
	if _, ok := c.Get("k1"); ok {
		t.Fatalf("expected miss after TTL")
	}
	if c.Len() != 0 {
		t.Fatalf("expected entry removed on expired Get, len=%d", c.Len())
	}
}

func TestCache_PutRefreshesTTL(t *testing.T) {
	c := cache.NewCache(8, time.Minute)
	now := time.Unix(1_700_000_000, 0)
	c.SetNowFunc(func() time.Time { return now })

	c.Put("k1", "v1")
	now = now.Add(45 * time.Second)
	c.Put("k1", "v2") // refresh TTL
	now = now.Add(30 * time.Second)
	got, ok := c.Get("k1")
	if !ok || got != "v2" {
		t.Fatalf("expected refreshed hit (v2), got (%v, %v)", got, ok)
	}
}

func TestCache_LRUEvictionOnCapacity(t *testing.T) {
	c := cache.NewCache(3, time.Minute)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Put("c", 3)
	// Touch "a" so it becomes most-recently-used.
	if _, ok := c.Get("a"); !ok {
		t.Fatalf("a should still be cached")
	}
	c.Put("d", 4) // should evict "b" (LRU)
	if _, ok := c.Get("b"); ok {
		t.Fatalf("expected b evicted")
	}
	if _, ok := c.Get("a"); !ok {
		t.Fatalf("a should still be cached")
	}
	if _, ok := c.Get("c"); !ok {
		t.Fatalf("c should still be cached")
	}
	if _, ok := c.Get("d"); !ok {
		t.Fatalf("d should be cached")
	}
	if c.Len() != 3 {
		t.Fatalf("expected len=3 after eviction, got %d", c.Len())
	}
}

func TestCache_PassThroughZeroCapacity(t *testing.T) {
	c := cache.NewCache(0, time.Minute)
	c.Put("k", "v")
	if _, ok := c.Get("k"); ok {
		t.Fatalf("zero-capacity cache must always miss")
	}
}

func TestCache_PassThroughZeroTTL(t *testing.T) {
	c := cache.NewCache(10, 0)
	c.Put("k", "v")
	if _, ok := c.Get("k"); ok {
		t.Fatalf("zero-TTL cache must always miss")
	}
}

func TestCache_NilReceiverSafe(t *testing.T) {
	var c *cache.Cache
	if _, ok := c.Get("k"); ok {
		t.Fatalf("nil cache must miss")
	}
	c.Put("k", "v") // must not panic
	if c.Invalidate("k") {
		t.Fatalf("nil Invalidate must return false")
	}
	if c.Len() != 0 {
		t.Fatalf("nil Len must be 0")
	}
}

func TestCache_DefaultsMatchPRD(t *testing.T) {
	if cache.DefaultCapacity != 10_000 {
		t.Errorf("PRD US-221 mandates 10k entries, got %d", cache.DefaultCapacity)
	}
	if cache.DefaultTTL != 5*time.Minute {
		t.Errorf("PRD US-221 mandates 5-minute TTL, got %s", cache.DefaultTTL)
	}
	c := cache.NewDefault()
	c.Put("k", "v")
	if _, ok := c.Get("k"); !ok {
		t.Fatalf("DefaultLimiter must hit immediately after Put")
	}
}

func TestCache_Invalidate(t *testing.T) {
	c := cache.NewCache(8, time.Minute)
	c.Put("k", "v")
	if !c.Invalidate("k") {
		t.Fatalf("expected Invalidate to remove the entry")
	}
	if _, ok := c.Get("k"); ok {
		t.Fatalf("expected miss after Invalidate")
	}
	if c.Invalidate("k") {
		t.Fatalf("second Invalidate should return false")
	}
}

func TestCache_Reset(t *testing.T) {
	c := cache.NewCache(8, time.Minute)
	c.Put("a", 1)
	c.Put("b", 2)
	c.Reset()
	if c.Len() != 0 {
		t.Fatalf("expected empty after Reset, len=%d", c.Len())
	}
}

func TestCache_InvalidatePrefix(t *testing.T) {
	c := cache.NewCache(16, time.Minute)
	// Mimic the canonical key shape: `<rid>@<version>#<hash>`.
	c.Put("ri.fn.alpha@1.0.0#aaa", "v1")
	c.Put("ri.fn.alpha@1.0.0#bbb", "v2")
	c.Put("ri.fn.alpha@2.0.0#ccc", "v3")
	c.Put("ri.fn.beta@1.0.0#ddd", "v4")

	removed := c.InvalidatePrefix("ri.fn.alpha@")
	if removed != 3 {
		t.Errorf("expected 3 entries removed, got %d", removed)
	}
	if _, ok := c.Get("ri.fn.alpha@1.0.0#aaa"); ok {
		t.Errorf("alpha entry should be gone")
	}
	if v, ok := c.Get("ri.fn.beta@1.0.0#ddd"); !ok || v != "v4" {
		t.Errorf("beta entry must survive prefix flush, got (%v, %v)", v, ok)
	}
	if c.Len() != 1 {
		t.Errorf("expected len=1 after prefix flush, got %d", c.Len())
	}
}

func TestCache_InvalidatePrefixVersionScoped(t *testing.T) {
	c := cache.NewCache(8, time.Minute)
	c.Put("ri.fn.alpha@1.0.0#aaa", "v1")
	c.Put("ri.fn.alpha@2.0.0#bbb", "v2")

	if removed := c.InvalidatePrefix("ri.fn.alpha@1.0.0#"); removed != 1 {
		t.Errorf("expected 1 entry removed for version-scoped prefix, got %d", removed)
	}
	if _, ok := c.Get("ri.fn.alpha@2.0.0#bbb"); !ok {
		t.Errorf("v2 entry must survive a v1-scoped flush")
	}
}

func TestCache_InvalidatePrefixEmptyIsNoOp(t *testing.T) {
	c := cache.NewCache(8, time.Minute)
	c.Put("ri.fn.alpha@1.0.0#aaa", "v1")
	if removed := c.InvalidatePrefix(""); removed != 0 {
		t.Errorf("empty prefix must return 0, got %d", removed)
	}
	if c.Len() != 1 {
		t.Errorf("empty prefix must NOT clear the cache, len=%d", c.Len())
	}
}

func TestCache_InvalidatePrefixNoMatchReturnsZero(t *testing.T) {
	c := cache.NewCache(8, time.Minute)
	c.Put("ri.fn.alpha@1.0.0#aaa", "v1")
	if removed := c.InvalidatePrefix("ri.fn.beta@"); removed != 0 {
		t.Errorf("no match must return 0, got %d", removed)
	}
	if c.Len() != 1 {
		t.Errorf("no-match flush must not clear the cache, len=%d", c.Len())
	}
}

func TestCache_InvalidatePrefixNilSafe(t *testing.T) {
	var c *cache.Cache
	if removed := c.InvalidatePrefix("ri.fn"); removed != 0 {
		t.Errorf("nil cache must return 0, got %d", removed)
	}
}

func TestCache_InvalidatePrefixPassThrough(t *testing.T) {
	c := cache.NewCache(0, time.Minute)
	if removed := c.InvalidatePrefix("ri.fn"); removed != 0 {
		t.Errorf("pass-through cache must return 0, got %d", removed)
	}
}

func TestKey_Stable(t *testing.T) {
	k1 := cache.Key("ri.fn", "1.0.0", map[string]interface{}{"a": 1, "b": 2})
	k2 := cache.Key("ri.fn", "1.0.0", map[string]interface{}{"b": 2, "a": 1})
	if k1 != k2 {
		t.Fatalf("expected key stability across map insertion order, got\n  %s\n  %s", k1, k2)
	}
}

func TestKey_NestedMapsCanonicalised(t *testing.T) {
	k1 := cache.Key("ri.fn", "1.0.0", map[string]interface{}{
		"a": map[string]interface{}{"x": 1, "y": 2},
		"b": []interface{}{1, 2, 3},
	})
	k2 := cache.Key("ri.fn", "1.0.0", map[string]interface{}{
		"b": []interface{}{1, 2, 3},
		"a": map[string]interface{}{"y": 2, "x": 1},
	})
	if k1 != k2 {
		t.Fatalf("nested maps must canonicalise: %s vs %s", k1, k2)
	}
}

func TestKey_DifferentParamsDifferentKey(t *testing.T) {
	k1 := cache.Key("ri.fn", "1.0.0", map[string]interface{}{"a": 1})
	k2 := cache.Key("ri.fn", "1.0.0", map[string]interface{}{"a": 2})
	if k1 == k2 {
		t.Fatalf("different params must produce different keys")
	}
}

func TestKey_DifferentVersionsDifferentKey(t *testing.T) {
	params := map[string]interface{}{"a": 1}
	k1 := cache.Key("ri.fn", "1.0.0", params)
	k2 := cache.Key("ri.fn", "1.0.1", params)
	if k1 == k2 {
		t.Fatalf("version pinning must produce different keys")
	}
}

func TestKey_EmptyParamsDeterministic(t *testing.T) {
	k1 := cache.Key("ri.fn", "1.0.0", nil)
	k2 := cache.Key("ri.fn", "1.0.0", map[string]interface{}{})
	if k1 != k2 {
		t.Fatalf("nil and empty maps must hash identically: %s vs %s", k1, k2)
	}
}

func TestKey_VersionOmitted(t *testing.T) {
	k := cache.Key("ri.fn", "", map[string]interface{}{"a": 1})
	if k == "" {
		t.Fatalf("Key with empty version should still produce a deterministic string")
	}
	// Same call must produce the same key.
	if k != cache.Key("ri.fn", "", map[string]interface{}{"a": 1}) {
		t.Fatalf("Key must be deterministic")
	}
}

func TestCache_ConcurrentAccess(t *testing.T) {
	c := cache.NewCache(64, time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := cache.Key("ri.fn", "1.0.0", map[string]interface{}{"i": i, "j": j % 8})
				if _, ok := c.Get(key); !ok {
					c.Put(key, j)
				}
			}
		}(i)
	}
	wg.Wait()
	// 16 goroutines × 8 unique j values = 128 distinct keys, capped at 64.
	if c.Len() > 64 {
		t.Fatalf("LRU cap should hold len<=64, got %d", c.Len())
	}
}
