package auth

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// DefaultRoleResolverMaxSize bounds the role cache so a flood of distinct
// users (e.g. malicious bot probing) cannot grow it without limit.
const DefaultRoleResolverMaxSize = 1000

// RoleResolver loads global and scoped role grants for a user, with a small
// TTL cache to amortize the cost across requests in the same window. The
// cache is bounded by an LRU policy so memory cannot grow without limit.
type RoleResolver struct {
	repo    UserRepository
	ttl     time.Duration
	maxSize int

	mu    sync.Mutex
	cache map[string]*list.Element
	lru   *list.List
}

type roleCacheEntry struct {
	userID  string
	global  []string
	scoped  map[string]string
	expires time.Time
}

// NewRoleResolver constructs a resolver with the default cache bound. A nil
// repo is allowed and produces a no-op resolver: every Resolve call returns
// empty role lists with no error. This is useful for dev mode where role
// lookups are skipped entirely.
func NewRoleResolver(repo UserRepository, ttl time.Duration) *RoleResolver {
	return NewRoleResolverWithSize(repo, ttl, DefaultRoleResolverMaxSize)
}

// NewRoleResolverWithSize constructs a resolver with an explicit cache bound.
// A non-positive maxSize falls back to DefaultRoleResolverMaxSize.
func NewRoleResolverWithSize(repo UserRepository, ttl time.Duration, maxSize int) *RoleResolver {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if maxSize <= 0 {
		maxSize = DefaultRoleResolverMaxSize
	}
	return &RoleResolver{
		repo:    repo,
		ttl:     ttl,
		maxSize: maxSize,
		cache:   make(map[string]*list.Element, maxSize),
		lru:     list.New(),
	}
}

// MaxSize returns the configured cache bound.
func (r *RoleResolver) MaxSize() int {
	if r == nil {
		return 0
	}
	return r.maxSize
}

// CacheSize returns the current number of entries in the cache.
func (r *RoleResolver) CacheSize() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lru.Len()
}

// Resolve returns (globalRoles, ontologyScopedRoles, error) for a user.
// It uses a TTL+LRU cache. Missing users yield empty results, not an error,
// because authentication has already happened upstream and a missing role row
// just means "no roles granted yet".
func (r *RoleResolver) Resolve(ctx context.Context, userID string) ([]string, map[string]string, error) {
	if r == nil || r.repo == nil {
		return nil, nil, nil
	}

	if userID == "" {
		return nil, nil, nil
	}

	r.mu.Lock()
	if elem, ok := r.cache[userID]; ok {
		entry := elem.Value.(*roleCacheEntry)
		if time.Now().Before(entry.expires) {
			r.lru.MoveToFront(elem)
			global := cloneStrings(entry.global)
			scoped := cloneStringMap(entry.scoped)
			r.mu.Unlock()
			return global, scoped, nil
		}
		// Expired — drop and fall through to refresh.
		r.lru.Remove(elem)
		delete(r.cache, userID)
	}
	r.mu.Unlock()

	global, err := r.repo.ListUserRoles(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	scoped, err := r.repo.ListUserOntologyRoles(ctx, userID)
	if err != nil {
		return nil, nil, err
	}

	r.mu.Lock()
	// Re-check in case a concurrent caller already populated the entry while
	// we were doing the repo lookup.
	if elem, ok := r.cache[userID]; ok {
		r.lru.Remove(elem)
		delete(r.cache, userID)
	}
	entry := &roleCacheEntry{
		userID:  userID,
		global:  cloneStrings(global),
		scoped:  cloneStringMap(scoped),
		expires: time.Now().Add(r.ttl),
	}
	elem := r.lru.PushFront(entry)
	r.cache[userID] = elem
	// Evict oldest entries until we are within the bound.
	for r.lru.Len() > r.maxSize {
		oldest := r.lru.Back()
		if oldest == nil {
			break
		}
		r.lru.Remove(oldest)
		delete(r.cache, oldest.Value.(*roleCacheEntry).userID)
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
	if elem, ok := r.cache[userID]; ok {
		r.lru.Remove(elem)
		delete(r.cache, userID)
	}
	r.mu.Unlock()
}

// InvalidateAll drops every cached entry.
func (r *RoleResolver) InvalidateAll() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.cache = make(map[string]*list.Element, r.maxSize)
	r.lru = list.New()
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
