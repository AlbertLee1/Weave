package security

import (
	"testing"
	"time"
)

func TestDecisionCache_PutGet(t *testing.T) {
	c := NewDecisionCache(8, time.Minute)

	if _, ok := c.Get(1); ok {
		t.Fatalf("expected miss on empty cache")
	}

	c.Put(1, true)
	got, ok := c.Get(1)
	if !ok || !got {
		t.Fatalf("expected hit allow=true, got ok=%v allow=%v", ok, got)
	}

	c.Put(1, false)
	got, ok = c.Get(1)
	if !ok || got {
		t.Fatalf("expected hit allow=false (refreshed), got ok=%v allow=%v", ok, got)
	}

	st := c.Stats()
	if st.Hits != 2 {
		t.Errorf("hits=%d, want 2", st.Hits)
	}
	if st.Misses != 1 {
		t.Errorf("misses=%d, want 1", st.Misses)
	}
	if st.Size != 1 {
		t.Errorf("size=%d, want 1", st.Size)
	}
}

func TestDecisionCache_TTLLazyExpiry(t *testing.T) {
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	c := NewDecisionCache(4, time.Minute)
	c.SetClock(func() time.Time { return now })

	c.Put(42, true)
	if got, ok := c.Get(42); !ok || !got {
		t.Fatalf("immediate Get: ok=%v allow=%v", ok, got)
	}

	now = now.Add(2 * time.Minute)
	// Get is intentionally lazy and still serves the entry; the eviction
	// pass is what actually drops it.
	if got, ok := c.Get(42); !ok || !got {
		t.Fatalf("Get is lazy and should still serve before reap, got ok=%v allow=%v", ok, got)
	}
	if removed := c.ReapExpired(); removed != 1 {
		t.Fatalf("ReapExpired = %d, want 1", removed)
	}
	if _, ok := c.Get(42); ok {
		t.Fatalf("expected miss after Reap")
	}
}

func TestDecisionCache_OverflowPurges(t *testing.T) {
	c := NewDecisionCache(2, time.Minute)

	c.Put(1, true)
	c.Put(2, true)
	c.Put(3, true)

	st := c.Stats()
	if st.Size > 2 {
		t.Fatalf("expected size <= max(2) after overflow, got %d", st.Size)
	}
}

func TestDecisionCache_ReapExpired(t *testing.T) {
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	c := NewDecisionCache(8, time.Minute)
	c.SetClock(func() time.Time { return now })

	c.Put(1, true)
	c.Put(2, true)
	if got := c.ReapExpired(); got != 0 {
		t.Fatalf("ReapExpired before TTL = %d, want 0", got)
	}
	now = now.Add(2 * time.Minute)
	if got := c.ReapExpired(); got != 2 {
		t.Fatalf("ReapExpired after TTL = %d, want 2", got)
	}
	if st := c.Stats(); st.Size != 0 {
		t.Fatalf("size after Reap = %d, want 0", st.Size)
	}
}

func TestDecisionCache_Reset(t *testing.T) {
	c := NewDecisionCache(8, time.Minute)
	c.Put(1, true)
	c.Put(2, false)
	c.Get(1)
	c.Get(99)

	c.Reset()
	st := c.Stats()
	if st.Hits != 0 || st.Misses != 0 || st.Size != 0 {
		t.Fatalf("stats after Reset = %+v, want all zero", st)
	}
	if _, ok := c.Get(1); ok {
		t.Fatalf("1 should be gone after Reset")
	}
}

func TestDecisionCache_HitRate(t *testing.T) {
	c := NewDecisionCache(4, time.Minute)
	if got := c.HitRate(); got != 0 {
		t.Fatalf("empty HitRate = %v, want 0", got)
	}

	c.Put(1, true)
	c.Get(1)
	c.Get(1)
	c.Get(2)

	rate := c.HitRate()
	want := 2.0 / 3.0
	if rate < want-1e-9 || rate > want+1e-9 {
		t.Fatalf("HitRate = %v, want %v", rate, want)
	}
}

func TestDecisionCache_Defaults(t *testing.T) {
	c := NewDecisionCache(0, 0)

	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	c.SetClock(func() time.Time { return now })

	c.Put(1, true)
	if _, ok := c.Get(1); !ok {
		t.Fatalf("expected hit immediately after Put")
	}

	now = now.Add(4 * time.Minute)
	if got := c.ReapExpired(); got != 0 {
		t.Fatalf("ReapExpired at 4m (default TTL 5m) = %d, want 0", got)
	}
	if _, ok := c.Get(1); !ok {
		t.Fatalf("expected hit at 4m (default TTL is 5m)")
	}

	now = now.Add(2 * time.Minute)
	if got := c.ReapExpired(); got != 1 {
		t.Fatalf("ReapExpired past 5m = %d, want 1", got)
	}
	if _, ok := c.Get(1); ok {
		t.Fatalf("expected miss past 5m default TTL")
	}
}
