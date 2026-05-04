package oms

import (
	"context"
	"log"
	"sync"
)

// FunctionCacheInvalidator drops cached Function results whose authors
// flagged an upstream ObjectType in the Function's DependsOn list (US-425).
// It is consulted by the funnel SetOnChange callback after every applied
// EditBatch — when a touched ObjectType is in the dependency set of any
// Function in the same ontology, every cache entry keyed on that
// Function's RID is dropped via FunctionResultCache.InvalidatePrefix.
//
// The invalidator caches the (ontology, objectType) → []functionRID
// reverse map per ontology so a hot path doesn't list every Function on
// every edit. The cache is repopulated lazily via Repository.ListFunctions
// and invalidated by the publish-time hook (Refresh) so a freshly added
// dependency takes effect on the next object change without a server
// restart. A nil receiver, nil cache, or nil repo all degrade to a no-op
// — same shape every other optional oms hook follows.
type FunctionCacheInvalidator struct {
	repo  Repository
	cache FunctionResultCache

	mu    sync.RWMutex
	index map[string]map[string][]string // ontologyAPIName → objectType → []fn RID
}

// NewFunctionCacheInvalidator wires the invalidator. Either argument may
// be nil; the resulting invalidator is a no-op until both are supplied,
// matching the degraded-mode contract every optional handler hook follows.
func NewFunctionCacheInvalidator(repo Repository, cache FunctionResultCache) *FunctionCacheInvalidator {
	return &FunctionCacheInvalidator{
		repo:  repo,
		cache: cache,
		index: map[string]map[string][]string{},
	}
}

// OnObjectChange is the funnel SetOnChange callback. When any Function in
// the ontology lists `objectType` in its DependsOn set, every cache entry
// keyed on that Function's RID is dropped. Errors from the repository
// surface only on the log so a downstream observability fan-out
// (broadcast, websocket, notifications) cannot stall on a transient list
// failure — cache freshness is best-effort layered on top of the 5-minute
// TTL ceiling, NOT a correctness gate.
func (inv *FunctionCacheInvalidator) OnObjectChange(ctx context.Context, ontologyAPIName, objectType string) {
	if inv == nil || inv.cache == nil || inv.repo == nil {
		return
	}
	if ontologyAPIName == "" || objectType == "" {
		return
	}
	rids, err := inv.lookup(ctx, ontologyAPIName, objectType)
	if err != nil {
		log.Printf("[function-cache] list functions for %q: %v", ontologyAPIName, err)
		return
	}
	for _, fnRID := range rids {
		inv.cache.InvalidatePrefix(fnRID + "@")
	}
}

// Refresh drops the cached reverse-index entry for the given ontology so
// the next OnObjectChange call rebuilds it. The publish-time hook
// (Create/Update/DeleteFunction handlers) calls Refresh so a freshly
// added or removed DependsOn entry takes effect on the next object
// change without a process restart.
//
// Empty ontologyAPIName clears the entire index (used by full-table
// admin reloads).
func (inv *FunctionCacheInvalidator) Refresh(ontologyAPIName string) {
	if inv == nil {
		return
	}
	inv.mu.Lock()
	defer inv.mu.Unlock()
	if ontologyAPIName == "" {
		inv.index = map[string]map[string][]string{}
		return
	}
	delete(inv.index, ontologyAPIName)
}

// lookup returns the cached `objectType → []fnRID` slice for the
// ontology, populating the entry on the first request.
func (inv *FunctionCacheInvalidator) lookup(ctx context.Context, ontologyAPIName, objectType string) ([]string, error) {
	inv.mu.RLock()
	if om, ok := inv.index[ontologyAPIName]; ok {
		rids := om[objectType]
		inv.mu.RUnlock()
		return rids, nil
	}
	inv.mu.RUnlock()

	fns, err := inv.repo.ListFunctions(ctx, ontologyAPIName)
	if err != nil {
		return nil, err
	}
	om := buildDependsOnIndex(fns)

	inv.mu.Lock()
	inv.index[ontologyAPIName] = om
	inv.mu.Unlock()

	return om[objectType], nil
}

// buildDependsOnIndex inverts the per-Function DependsOn slice into a
// map keyed by ObjectType API name → []fn RID. Pure functions with an
// empty DependsOn list contribute nothing — they are handled by the TTL
// ceiling alone.
func buildDependsOnIndex(fns []Function) map[string][]string {
	out := map[string][]string{}
	for _, fn := range fns {
		if len(fn.DependsOn) == 0 {
			continue
		}
		seen := map[string]struct{}{}
		for _, ot := range fn.DependsOn {
			if ot == "" {
				continue
			}
			if _, dup := seen[ot]; dup {
				continue
			}
			seen[ot] = struct{}{}
			out[ot] = append(out[ot], fn.RID)
		}
	}
	return out
}
