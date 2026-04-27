package secrets

import (
	"context"
	"sync"
	"time"
)

// CachingProvider wraps an inner Provider with a per-key TTL cache so
// repeated lookups within the TTL window don't re-hit Vault / the
// secrets directory. After the TTL elapses, the next call refreshes
// from the inner provider — this is the rotation-awareness mechanism
// the AC describes: rotate the secret in Vault, wait at most one TTL,
// the next read pulls the new value automatically (US-278).
//
// TTL=0 disables caching (every Get goes straight to the inner
// provider). Negative values panic at construction.
type CachingProvider struct {
	inner Provider
	ttl   time.Duration
	clock func() time.Time

	mu    sync.RWMutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

// NewCachingProvider returns a TTL-cached wrapper around inner. ttl
// must be >= 0; pass 0 for no caching.
func NewCachingProvider(inner Provider, ttl time.Duration) *CachingProvider {
	if ttl < 0 {
		panic("NewCachingProvider: negative ttl")
	}
	return &CachingProvider{
		inner: inner,
		ttl:   ttl,
		clock: time.Now,
		cache: make(map[string]cacheEntry),
	}
}

func (p *CachingProvider) Name() string {
	if p.inner == nil {
		return "cache(nil)"
	}
	return "cache(" + p.inner.Name() + ")"
}

func (p *CachingProvider) Get(ctx context.Context, key string) (string, error) {
	if p.ttl == 0 {
		return p.inner.Get(ctx, key)
	}
	now := p.clock()

	p.mu.RLock()
	if e, ok := p.cache[key]; ok && now.Before(e.expiresAt) {
		p.mu.RUnlock()
		return e.value, nil
	}
	p.mu.RUnlock()

	value, err := p.inner.Get(ctx, key)
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	p.cache[key] = cacheEntry{value: value, expiresAt: now.Add(p.ttl)}
	p.mu.Unlock()
	return value, nil
}

// Invalidate drops the cache entry for key (e.g. after an admin push
// changes the secret out-of-band). A subsequent Get re-reads the inner
// provider.
func (p *CachingProvider) Invalidate(key string) {
	p.mu.Lock()
	delete(p.cache, key)
	p.mu.Unlock()
}

// Reset drops every cached entry — used at config reload.
func (p *CachingProvider) Reset() {
	p.mu.Lock()
	p.cache = make(map[string]cacheEntry)
	p.mu.Unlock()
}
