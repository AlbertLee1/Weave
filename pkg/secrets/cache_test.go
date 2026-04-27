package secrets

import (
	"context"
	"errors"
	"testing"
	"time"
)

// counterProvider tracks the number of inner Get calls so tests can
// assert the cache hit/miss behaviour without timing flakes.
type counterProvider struct {
	value string
	calls int
	err   error
}

func (c *counterProvider) Name() string { return "counter" }
func (c *counterProvider) Get(_ context.Context, _ string) (string, error) {
	c.calls++
	if c.err != nil {
		return "", c.err
	}
	return c.value, nil
}

func TestCachingProvider_HitsCacheWithinTTL(t *testing.T) {
	inner := &counterProvider{value: "v1"}
	cp := NewCachingProvider(inner, time.Hour)

	for i := 0; i < 5; i++ {
		got, err := cp.Get(context.Background(), "k")
		if err != nil || got != "v1" {
			t.Fatalf("unexpected: %q %v", got, err)
		}
	}
	if inner.calls != 1 {
		t.Errorf("want 1 inner call, got %d", inner.calls)
	}
}

func TestCachingProvider_RefreshesAfterTTL(t *testing.T) {
	inner := &counterProvider{value: "v1"}
	cp := NewCachingProvider(inner, time.Minute)
	now := time.Unix(1_000, 0)
	cp.clock = func() time.Time { return now }

	if v, _ := cp.Get(context.Background(), "k"); v != "v1" {
		t.Fatalf("first Get: %q", v)
	}
	if inner.calls != 1 {
		t.Fatal("first Get should hit inner")
	}

	// Within TTL: cached.
	now = now.Add(30 * time.Second)
	_, _ = cp.Get(context.Background(), "k")
	if inner.calls != 1 {
		t.Errorf("within TTL: want 1 inner call, got %d", inner.calls)
	}

	// After TTL + simulated rotation: refresh and pick up new value.
	now = now.Add(2 * time.Minute)
	inner.value = "v2-rotated"
	got, err := cp.Get(context.Background(), "k")
	if err != nil {
		t.Fatalf("post-TTL: %v", err)
	}
	if got != "v2-rotated" {
		t.Errorf("post-TTL: want rotated value, got %q", got)
	}
	if inner.calls != 2 {
		t.Errorf("post-TTL: want 2 inner calls, got %d", inner.calls)
	}
}

func TestCachingProvider_TTLZeroDisabled(t *testing.T) {
	inner := &counterProvider{value: "v"}
	cp := NewCachingProvider(inner, 0)
	for i := 0; i < 3; i++ {
		_, _ = cp.Get(context.Background(), "k")
	}
	if inner.calls != 3 {
		t.Errorf("ttl=0 should bypass cache; want 3 calls, got %d", inner.calls)
	}
}

func TestCachingProvider_DoesNotCacheErrors(t *testing.T) {
	inner := &counterProvider{err: ErrSecretNotFound}
	cp := NewCachingProvider(inner, time.Hour)
	for i := 0; i < 3; i++ {
		_, err := cp.Get(context.Background(), "missing")
		if !errors.Is(err, ErrSecretNotFound) {
			t.Errorf("want ErrSecretNotFound, got %v", err)
		}
	}
	if inner.calls != 3 {
		t.Errorf("errors should NOT be cached; want 3 calls, got %d", inner.calls)
	}
}

func TestCachingProvider_Invalidate(t *testing.T) {
	inner := &counterProvider{value: "v1"}
	cp := NewCachingProvider(inner, time.Hour)
	_, _ = cp.Get(context.Background(), "k")
	_, _ = cp.Get(context.Background(), "k")
	if inner.calls != 1 {
		t.Fatal("setup")
	}
	cp.Invalidate("k")
	_, _ = cp.Get(context.Background(), "k")
	if inner.calls != 2 {
		t.Errorf("after Invalidate, want a fresh fetch; got %d total calls", inner.calls)
	}
}

func TestCachingProvider_Reset(t *testing.T) {
	inner := &counterProvider{value: "v1"}
	cp := NewCachingProvider(inner, time.Hour)
	_, _ = cp.Get(context.Background(), "a")
	_, _ = cp.Get(context.Background(), "b")
	_, _ = cp.Get(context.Background(), "a")
	if inner.calls != 2 {
		t.Fatalf("setup: want 2, got %d", inner.calls)
	}
	cp.Reset()
	_, _ = cp.Get(context.Background(), "a")
	if inner.calls != 3 {
		t.Errorf("after Reset, want fresh fetch; got %d", inner.calls)
	}
}

func TestCachingProvider_NameWraps(t *testing.T) {
	cp := NewCachingProvider(NewEnvProvider(), time.Hour)
	if got := cp.Name(); got != "cache(env)" {
		t.Errorf("name: got %q, want cache(env)", got)
	}
}

func TestNewCachingProvider_NegativeTTLPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on negative ttl")
		}
	}()
	_ = NewCachingProvider(NewEnvProvider(), -1)
}
