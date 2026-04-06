package auth

import (
	"context"
	"sync"
	"time"
)

// RoleResolver loads global and scoped role grants for a user, with a small
// TTL cache to amortize the cost across requests in the same window.
type RoleResolver struct {
	repo  UserRepository
	ttl   time.Duration
	mu    sync.RWMutex
	cache map[string]roleCacheEntry
}

type roleCacheEntry struct {
	global  []string
	scoped  map[string]string
	expires time.Time
}

// NewRoleResolver constructs a resolver. A nil repo is allowed and produces a
// no-op resolver: every Resolve call returns empty role lists with no error.
// This is useful for dev mode where role lookups are skipped entirely.
func NewRoleResolver(repo UserRepository, ttl time.Duration) *RoleResolver {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &RoleResolver{
		repo:  repo,
		ttl:   ttl,
		cache: map[string]roleCacheEntry{},
	}
}

// Resolve returns (globalRoles, ontologyScopedRoles, error) for a user.
// It uses a simple TTL cache. Missing users yield empty results, not an error,
// because authentication has already happened upstream and a missing role row
// just means "no roles granted yet".
func (r *RoleResolver) Resolve(ctx context.Context, userID string) ([]string, map[string]string, error) {
	if r == nil || r.repo == nil {
		return nil, nil, nil
	}

	if userID == "" {
		return nil, nil, nil
	}

	r.mu.RLock()
	if entry, ok := r.cache[userID]; ok && time.Now().Before(entry.expires) {
		r.mu.RUnlock()
		return cloneStrings(entry.global), cloneStringMap(entry.scoped), nil
	}
	r.mu.RUnlock()

	global, err := r.repo.ListUserRoles(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	scoped, err := r.repo.ListUserOntologyRoles(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	r.mu.Lock()
	r.cache[userID] = roleCacheEntry{
		global:  cloneStrings(global),
		scoped:  cloneStringMap(scoped),
		expires: time.Now().Add(r.ttl),
	}
	r.mu.Unlock()

	return global, scoped, nil
}

// Invalidate drops the cache entry for a user. Call after a role write.
func (r *RoleResolver) Invalidate(userID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.cache, userID)
	r.mu.Unlock()
}

// InvalidateAll drops every cached entry.
func (r *RoleResolver) InvalidateAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.cache = map[string]roleCacheEntry{}
	r.mu.Unlock()
}

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
