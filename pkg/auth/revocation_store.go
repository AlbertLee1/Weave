package auth

import (
	"context"
	"errors"
	"sync"
	"time"
)

// US-491: JWT access-token revocation blacklist.
//
// Middleware consults a RevocationChecker before letting a verified JWT
// through. The checker is fronted by a short TTL cache so the hot path
// avoids one round-trip per request; admin POST /api/auth/tokens/{jti}/revoke
// inserts into the underlying store AND invalidates the cache so the next
// request sees the new state immediately on the same instance.

// ErrRevocationInvalid signals a malformed RevocationRecord (e.g. empty jti).
var ErrRevocationInvalid = errors.New("revocation: invalid record")

// RevocationRecord is the persisted shape of a revoked JTI. ExpiresAt is the
// original `exp` claim from the token; rows past that timestamp are pruned
// by ReapExpired since they would have failed JWT.Verify on their own.
type RevocationRecord struct {
	JTI       string
	UserID    string
	ExpiresAt time.Time
	RevokedAt time.Time
	Reason    string
}

// RevocationStore is the persistence interface for the revocation blacklist.
type RevocationStore interface {
	Revoke(ctx context.Context, rec RevocationRecord) error
	IsRevoked(ctx context.Context, jti string) (bool, error)
	ReapExpired(ctx context.Context, before time.Time) (int64, error)
}

// MemoryRevocationStore is an in-memory RevocationStore used by tests and the
// dev-mode bootstrap. Safe for concurrent use.
type MemoryRevocationStore struct {
	mu  sync.RWMutex
	rev map[string]RevocationRecord
}

// NewMemoryRevocationStore returns an empty in-memory store.
func NewMemoryRevocationStore() *MemoryRevocationStore {
	return &MemoryRevocationStore{rev: map[string]RevocationRecord{}}
}

// Revoke inserts a record into the blacklist. Idempotent: repeated calls for
// the same JTI overwrite the existing row so callers can update reason / exp.
func (s *MemoryRevocationStore) Revoke(_ context.Context, rec RevocationRecord) error {
	if rec.JTI == "" {
		return ErrRevocationInvalid
	}
	if rec.RevokedAt.IsZero() {
		rec.RevokedAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rev[rec.JTI] = rec
	return nil
}

// IsRevoked reports whether the given JTI is in the blacklist.
func (s *MemoryRevocationStore) IsRevoked(_ context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.rev[jti]
	return ok, nil
}

// ReapExpired removes rows whose ExpiresAt is at or before `before`. Returns
// the number of rows deleted.
func (s *MemoryRevocationStore) ReapExpired(_ context.Context, before time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for k, r := range s.rev {
		if !r.ExpiresAt.After(before) {
			delete(s.rev, k)
			n++
		}
	}
	return n, nil
}

// CachedRevocationChecker wraps a RevocationStore with a small TTL cache so
// per-request lookups don't fan out to the database. Cache misses fall
// through to the underlying store; Invalidate is called by the admin handler
// immediately after a successful Revoke so subsequent middleware checks on
// the same process see the new state without waiting for the TTL.
//
// A nil store yields a checker that always reports "not revoked" — used in
// degraded boot (no PG pool) so the middleware doesn't reject every request
// the moment AUTH_MODE=jwt is set without a backing table.
type CachedRevocationChecker struct {
	store RevocationStore
	ttl   time.Duration

	mu    sync.RWMutex
	cache map[string]cachedRevocation
}

type cachedRevocation struct {
	revoked bool
	expires time.Time
}

// NewCachedRevocationChecker wraps store with a TTL cache. A non-positive
// ttl defaults to 30s — long enough to absorb a burst of requests for the
// same JTI, short enough that operators don't need to bounce the process
// after a multi-instance revoke if cross-instance cache invalidation isn't
// in place yet.
func NewCachedRevocationChecker(store RevocationStore, ttl time.Duration) *CachedRevocationChecker {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &CachedRevocationChecker{
		store: store,
		ttl:   ttl,
		cache: map[string]cachedRevocation{},
	}
}

// IsRevoked checks the cache first; on miss / expiry falls through to the
// underlying store and caches the result. Errors are surfaced so the
// middleware can decide its own fail-policy (current implementation: fail
// open — log + let the request through, see Middleware).
func (c *CachedRevocationChecker) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if c == nil || c.store == nil {
		return false, nil
	}
	if jti == "" {
		return false, nil
	}
	now := time.Now()

	c.mu.RLock()
	entry, ok := c.cache[jti]
	c.mu.RUnlock()
	if ok && now.Before(entry.expires) {
		return entry.revoked, nil
	}

	revoked, err := c.store.IsRevoked(ctx, jti)
	if err != nil {
		return false, err
	}

	c.mu.Lock()
	c.cache[jti] = cachedRevocation{revoked: revoked, expires: now.Add(c.ttl)}
	c.mu.Unlock()
	return revoked, nil
}

// Invalidate drops the cached entry for jti. Called by the admin revoke
// handler so the next IsRevoked call goes back to the store on this process.
func (c *CachedRevocationChecker) Invalidate(jti string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	delete(c.cache, jti)
	c.mu.Unlock()
}

// CacheSize returns the current cached entry count. Exported for tests.
func (c *CachedRevocationChecker) CacheSize() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// Store exposes the underlying RevocationStore for handlers that need to
// perform the persisted write (e.g. admin revoke handler). Returns nil when
// the checker was constructed with a nil store.
func (c *CachedRevocationChecker) Store() RevocationStore {
	if c == nil {
		return nil
	}
	return c.store
}
