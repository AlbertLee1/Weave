package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// editOnlyResolverRepo is the narrow oms.Repository subset the
// editOnlyResolver needs. Local interface keeps tests stub-friendly —
// mirrors the linkPropagationResolverRepo / interfaceResolverRepo shape.
type editOnlyResolverRepo interface {
	ListOntologies(ctx context.Context) ([]oms.Ontology, error)
	ListObjectTypes(ctx context.Context, ontologyRID string) ([]oms.ObjectType, error)
	ListProperties(ctx context.Context, objectTypeRID string) ([]oms.Property, error)
}

// editOnlyResolver is the production-shape backing for the funnel
// Consumer's US-027 SetEditOnlyField hook (now extended by US-472 from
// "in test harness" to "running in main.go"). It caches an
// (objectTypeAPIName) -> set[propertyAPIName] map of fields flagged
// IsEditOnly=true in the live OMS schema so the consumer's per-edit
// IsEditOnly probe is O(1) without a PG round-trip on every ingest write.
//
// The funnel callback signature `func(objectType, field string) bool`
// drops the ontology dimension; if two ontologies declare the same OT
// APIName with different IsEditOnly flags, the cache OR-aggregates
// (over-protection is the safer failure mode than silent leakage of a
// user-managed field through an ingest path).
type editOnlyResolver struct {
	repo   editOnlyResolverRepo
	mu     sync.RWMutex
	fields map[string]map[string]bool
}

// newEditOnlyResolver builds an empty resolver. Refresh() must be called
// at least once before IsEditOnly returns useful answers; runEditOnlyRefreshLoop
// keeps the cache fresh against admin schema changes without a restart.
func newEditOnlyResolver(repo editOnlyResolverRepo) *editOnlyResolver {
	return &editOnlyResolver{
		repo:   repo,
		fields: map[string]map[string]bool{},
	}
}

// Refresh rebuilds the cache from scratch by walking every ontology, every
// object type, and the IsEditOnly flag on every property. A ListOntologies
// failure aborts and surfaces — the resolver keeps its previous cache so a
// transient PG hiccup does not strip protection from all editOnly fields.
// Per-ObjectType ListProperties failures are logged and skipped (partial
// cache > empty cache); a subsequent successful refresh repairs the gap.
func (r *editOnlyResolver) Refresh(ctx context.Context) error {
	if r == nil || r.repo == nil {
		return nil
	}
	onts, err := r.repo.ListOntologies(ctx)
	if err != nil {
		return err
	}
	next := map[string]map[string]bool{}
	for _, o := range onts {
		ots, err := r.repo.ListObjectTypes(ctx, o.RID)
		if err != nil {
			log.Printf("[edit-only] list objectTypes for %s: %v", o.APIName, err)
			continue
		}
		for _, ot := range ots {
			props, err := r.repo.ListProperties(ctx, ot.RID)
			if err != nil {
				log.Printf("[edit-only] list properties for %s/%s: %v", o.APIName, ot.APIName, err)
				continue
			}
			for _, p := range props {
				if !p.IsEditOnly {
					continue
				}
				bucket, ok := next[ot.APIName]
				if !ok {
					bucket = map[string]bool{}
					next[ot.APIName] = bucket
				}
				bucket[p.APIName] = true
			}
		}
	}
	r.mu.Lock()
	r.fields = next
	r.mu.Unlock()
	return nil
}

// IsEditOnly answers the funnel Consumer's per-edit probe. Returns false for
// unknown (objectType, field) tuples and for nil receivers so a degraded
// boot (no OMS repo) silently disables the guard rather than panicking.
func (r *editOnlyResolver) IsEditOnly(objectType, field string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	bucket, ok := r.fields[objectType]
	if !ok {
		return false
	}
	return bucket[field]
}

// runEditOnlyRefreshLoop is the standard reaper-style driver — periodic
// Refresh with onError swallowed via the callback so the loop never wedges
// on a transient PG failure. Exits cleanly when ctx is cancelled. A nil
// resolver or non-positive interval makes the loop a no-op (returns
// immediately) so callers do not need to nil-check at every wiring site.
func runEditOnlyRefreshLoop(ctx context.Context, r *editOnlyResolver, interval time.Duration, onError func(error)) {
	if r == nil || interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.Refresh(ctx); err != nil {
				if onError != nil {
					onError(err)
				}
			}
		}
	}
}
