package quota

import (
	"sync"
	"testing"
	"time"
)

func TestLimiter_WithinLimit(t *testing.T) {
	l := NewLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !l.Allow("main") {
			t.Fatalf("call %d should be allowed (limit 3)", i)
		}
	}
}

func TestLimiter_RejectsOverLimit(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	if !l.Allow("main") || !l.Allow("main") {
		t.Fatalf("first two calls should be allowed")
	}
	if l.Allow("main") {
		t.Fatalf("third call should be rejected (limit 2)")
	}
}

func TestLimiter_KeyIsolation(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	if !l.Allow("realm-a") {
		t.Fatal("realm-a first call should be allowed")
	}
	if l.Allow("realm-a") {
		t.Fatal("realm-a second call should be rejected")
	}
	// A different realm must not see realm-a's exhausted bucket.
	if !l.Allow("realm-b") {
		t.Fatal("realm-b first call should be allowed despite realm-a being full")
	}
}

func TestLimiter_RollingWindowEvicts(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := NewLimiter(2, time.Minute)
	l.SetNowFunc(func() time.Time { return now })

	if !l.Allow("main") || !l.Allow("main") {
		t.Fatal("first two calls should be allowed")
	}
	if l.Allow("main") {
		t.Fatal("third call at same instant should be rejected")
	}

	// Advance past the window — both prior timestamps evict.
	now = now.Add(61 * time.Second)
	if !l.Allow("main") {
		t.Fatal("call after window rolls past should be allowed")
	}
	if !l.Allow("main") {
		t.Fatal("second call within fresh window should be allowed")
	}
	if l.Allow("main") {
		t.Fatal("third call within fresh window should be rejected")
	}
}

func TestLimiter_ZeroConfigPassesThrough(t *testing.T) {
	l := NewLimiter(0, time.Minute)
	for i := 0; i < 100; i++ {
		if !l.Allow("main") {
			t.Fatalf("limit=0 Limiter should pass through call %d", i)
		}
	}
	l2 := NewLimiter(5, 0)
	for i := 0; i < 100; i++ {
		if !l2.Allow("main") {
			t.Fatalf("window=0 Limiter should pass through call %d", i)
		}
	}
}

func TestLimiter_NilReceiverAllows(t *testing.T) {
	var l *Limiter
	if !l.Allow("main") {
		t.Fatal("nil Limiter should allow")
	}
}

func TestLimiter_DefaultLimiterMatchesPRDConstants(t *testing.T) {
	l := DefaultLimiter()
	if l.limit != DefaultLimit {
		t.Fatalf("DefaultLimiter.limit = %d, want %d", l.limit, DefaultLimit)
	}
	if l.window != DefaultWindow {
		t.Fatalf("DefaultLimiter.window = %v, want %v", l.window, DefaultWindow)
	}
	if DefaultLimit != 60 || DefaultWindow != time.Minute {
		t.Fatalf("PRD drift: DefaultLimit=%d DefaultWindow=%v (expected 60/minute)", DefaultLimit, DefaultWindow)
	}
}

func TestLimiter_Reset(t *testing.T) {
	l := NewLimiter(1, time.Minute)
	if !l.Allow("main") {
		t.Fatal("first call should be allowed")
	}
	if l.Allow("main") {
		t.Fatal("second call should be rejected pre-reset")
	}
	l.Reset()
	if !l.Allow("main") {
		t.Fatal("first call post-reset should be allowed")
	}
}

func TestLimiter_ConcurrentAllow(t *testing.T) {
	l := NewLimiter(100, time.Minute)
	var wg sync.WaitGroup
	var ok, denied int64
	var mu sync.Mutex

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			allowed := l.Allow("main")
			mu.Lock()
			defer mu.Unlock()
			if allowed {
				ok++
			} else {
				denied++
			}
		}()
	}
	wg.Wait()

	if ok != 100 {
		t.Fatalf("expected exactly 100 allowed under a limit=100 bucket, got allowed=%d denied=%d", ok, denied)
	}
	if denied != 100 {
		t.Fatalf("expected the remaining 100 denied, got allowed=%d denied=%d", ok, denied)
	}
}
