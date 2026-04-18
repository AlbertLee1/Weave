// Package computed evaluates OMS ComputedProperty rows against a live link
// graph and object index. Each call walks the declared source_link_rid from
// a single base primary key, then collapses the linked object set via the
// requested aggregation. Results are cached in-process for the TTL declared
// on the ComputedProperty row so a hot read path does not re-walk links or
// re-scan Bleve on every request.
package computed

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/oms"
)

// LinkedObjectsResolver resolves the set of target primary keys reached by
// walking a link type from the given sources. The production implementation
// is pkg/links.Resolver; tests swap in fakes. The RID-first contract mirrors
// the ComputedProperty row shape exactly (source_link_rid).
type LinkedObjectsResolver interface {
	ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error)
}

// LinkTypeLookup fetches LinkType metadata by RID so the Resolver can
// determine the target ObjectType whose Bleve index holds the numeric field
// being aggregated. *oms.PGRepository satisfies this interface directly.
type LinkTypeLookup interface {
	GetLinkType(ctx context.Context, rid string) (*oms.LinkType, error)
}

// IndexSearcher is the minimal Bleve-search surface the Resolver needs. Its
// production implementation is *index.Manager; tests back it with a map of
// in-memory bleve indexes keyed by ObjectType API name.
type IndexSearcher interface {
	Search(objectType string, req *bleve.SearchRequest) (*bleve.SearchResult, error)
}

// Value is a single computed property reading. ComputedAt is the wall-clock
// time at which the underlying aggregation last ran; API handlers surface it
// as the _computed_at field described in the US-202 acceptance criteria.
type Value struct {
	Value      interface{}
	ComputedAt time.Time
}

// Resolver evaluates oms.ComputedProperty rows with an in-process TTL cache.
// The zero value is not usable; construct one via NewResolver.
type Resolver struct {
	links   LinkedObjectsResolver
	lookup  LinkTypeLookup
	idx     IndexSearcher
	nowFunc func() time.Time

	mu    sync.Mutex
	cache map[string]cachedValue
}

type cachedValue struct {
	value      interface{}
	computedAt time.Time
	expiresAt  time.Time
}

// NewResolver wires a Resolver from its three runtime dependencies. The
// clock defaults to time.Now; tests override it with SetNowFunc.
func NewResolver(links LinkedObjectsResolver, lookup LinkTypeLookup, idx IndexSearcher) *Resolver {
	return &Resolver{
		links:   links,
		lookup:  lookup,
		idx:     idx,
		nowFunc: time.Now,
		cache:   make(map[string]cachedValue),
	}
}

// SetNowFunc overrides the wall clock. Tests use this to exercise cache TTL
// expiry deterministically; production callers should leave it alone.
func (r *Resolver) SetNowFunc(f func() time.Time) {
	r.nowFunc = f
}

// InvalidateAll drops every cached reading. Admin writes to a
// ComputedProperty row should call this to avoid serving stale values.
func (r *Resolver) InvalidateAll() {
	r.mu.Lock()
	r.cache = make(map[string]cachedValue)
	r.mu.Unlock()
}

// Evaluate returns the computed value for (cp, sourcePK), hitting the cache
// when a fresh entry exists and falling back to a live aggregation
// otherwise. Value.ComputedAt records when the returned value was computed.
func (r *Resolver) Evaluate(ctx context.Context, cp *oms.ComputedProperty, sourcePK string) (Value, error) {
	if cp == nil {
		return Value{}, fmt.Errorf("computed property is nil")
	}
	if err := cp.Validate(); err != nil {
		return Value{}, err
	}

	key := cacheKey(cp.RID, sourcePK)
	now := r.nowFunc()

	r.mu.Lock()
	if cached, ok := r.cache[key]; ok && now.Before(cached.expiresAt) {
		r.mu.Unlock()
		return Value{Value: cached.value, ComputedAt: cached.computedAt}, nil
	}
	r.mu.Unlock()

	val, err := r.compute(ctx, cp, sourcePK)
	if err != nil {
		return Value{}, err
	}

	ttl := cp.CacheTTL()
	if ttl > 0 {
		r.mu.Lock()
		r.cache[key] = cachedValue{
			value:      val,
			computedAt: now,
			expiresAt:  now.Add(ttl),
		}
		r.mu.Unlock()
	}
	return Value{Value: val, ComputedAt: now}, nil
}

// compute executes a fresh aggregation; it never reads the cache.
func (r *Resolver) compute(ctx context.Context, cp *oms.ComputedProperty, sourcePK string) (interface{}, error) {
	targets, err := r.links.ResolveLinkedObjects(ctx, cp.SourceLinkRID, []string{sourcePK})
	if err != nil {
		return nil, fmt.Errorf("resolve link %q: %w", cp.SourceLinkRID, err)
	}

	aggType := strings.ToLower(cp.Aggregation.Type)
	if aggType == "count" {
		return int64(len(targets)), nil
	}

	if len(targets) == 0 {
		if aggType == "sum" {
			return float64(0), nil
		}
		return nil, nil
	}

	lt, err := r.lookup.GetLinkType(ctx, cp.SourceLinkRID)
	if err != nil {
		return nil, fmt.Errorf("lookup link type %q: %w", cp.SourceLinkRID, err)
	}
	if lt.TargetObjectType == "" {
		return nil, fmt.Errorf("link type %q has no target ObjectType", cp.SourceLinkRID)
	}

	searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery(targets))
	searchReq.Fields = []string{cp.Aggregation.Field}
	searchReq.Size = len(targets)
	res, err := r.idx.Search(lt.TargetObjectType, searchReq)
	if err != nil {
		return nil, fmt.Errorf("search target %q for field %q: %w", lt.TargetObjectType, cp.Aggregation.Field, err)
	}

	var (
		sum        float64
		count      int
		haveMinMax bool
		minV, maxV float64
	)
	for _, hit := range res.Hits {
		raw, ok := hit.Fields[cp.Aggregation.Field]
		if !ok || raw == nil {
			continue
		}
		num, ok := coerceNumeric(raw)
		if !ok {
			return nil, fmt.Errorf("computed property %q: field %q on %q is not numeric (got %T)", cp.APIName, cp.Aggregation.Field, lt.TargetObjectType, raw)
		}
		sum += num
		count++
		if !haveMinMax {
			minV, maxV = num, num
			haveMinMax = true
			continue
		}
		if num < minV {
			minV = num
		}
		if num > maxV {
			maxV = num
		}
	}

	if count == 0 {
		if aggType == "sum" {
			return float64(0), nil
		}
		return nil, nil
	}

	switch aggType {
	case "sum":
		return sum, nil
	case "avg":
		return sum / float64(count), nil
	case "min":
		return minV, nil
	case "max":
		return maxV, nil
	}
	return nil, fmt.Errorf("unsupported aggregation type %q", aggType)
}

func cacheKey(rid, pk string) string {
	return rid + "\x00" + pk
}

func coerceNumeric(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err == nil {
			return f, true
		}
	}
	return 0, false
}
