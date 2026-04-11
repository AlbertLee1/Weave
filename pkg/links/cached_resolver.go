package links

import (
	"context"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// LinkTypeCache memoizes LinkType metadata lookups used by the link resolver.
// The resolver consults this cache for GetLinkType and ListOutgoingLinkTypes
// before delegating to the underlying repository, which avoids hitting
// PostgreSQL on every linked-object traversal.
//
// Only successful reads are cached; errors always fall through to the repo so
// a newly created link type becomes immediately visible without explicit
// invalidation. Writes on the caller side should call InvalidateAll.
type LinkTypeCache struct {
	ttl     time.Duration
	mu      sync.RWMutex
	nowFunc func() time.Time

	byRID    map[string]linkTypeEntry
	outgoing map[string]outgoingEntry
}

type linkTypeEntry struct {
	value   *oms.LinkType
	expires time.Time
}

type outgoingEntry struct {
	value   []oms.LinkType
	expires time.Time
}

// NewLinkTypeCache returns a LinkTypeCache with the given TTL. A TTL of 0
// disables caching (every call falls through to the repo).
func NewLinkTypeCache(ttl time.Duration) *LinkTypeCache {
	return &LinkTypeCache{
		ttl:      ttl,
		nowFunc:  time.Now,
		byRID:    map[string]linkTypeEntry{},
		outgoing: map[string]outgoingEntry{},
	}
}

// SetNowFunc installs a test clock.
func (c *LinkTypeCache) SetNowFunc(f func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nowFunc = f
}

// InvalidateAll drops every cached link-type entry.
func (c *LinkTypeCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byRID = map[string]linkTypeEntry{}
	c.outgoing = map[string]outgoingEntry{}
}

// linkTypeFetcher is the narrow read surface the cache needs from a repository.
// Any *oms.Repository (and its CachedRepository decorator) satisfies it.
type linkTypeFetcher interface {
	GetLinkType(ctx context.Context, rid string) (*oms.LinkType, error)
	ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]oms.LinkType, error)
}

// GetLinkType returns the cached link type for rid, falling back to repo on
// miss or expiry.
func (c *LinkTypeCache) GetLinkType(ctx context.Context, repo linkTypeFetcher, rid string) (*oms.LinkType, error) {
	c.mu.RLock()
	if e, ok := c.byRID[rid]; ok && c.nowFunc().Before(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()

	lt, err := repo.GetLinkType(ctx, rid)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.byRID[rid] = linkTypeEntry{value: lt, expires: c.nowFunc().Add(c.ttl)}
	c.mu.Unlock()
	return lt, nil
}

// ListOutgoingLinkTypes returns the cached outgoing link types for
// objectTypeRID, falling back to repo on miss or expiry.
func (c *LinkTypeCache) ListOutgoingLinkTypes(ctx context.Context, repo linkTypeFetcher, objectTypeRID string) ([]oms.LinkType, error) {
	c.mu.RLock()
	if e, ok := c.outgoing[objectTypeRID]; ok && c.nowFunc().Before(e.expires) {
		c.mu.RUnlock()
		return e.value, nil
	}
	c.mu.RUnlock()

	lts, err := repo.ListOutgoingLinkTypes(ctx, objectTypeRID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.outgoing[objectTypeRID] = outgoingEntry{value: lts, expires: c.nowFunc().Add(c.ttl)}
	c.mu.Unlock()
	return lts, nil
}
