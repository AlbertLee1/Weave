package links_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
)

// countingRepo wraps a minimal repo with invocation counters for the link
// metadata methods the resolver consults. It reuses the mockRepo fields in
// resolver_test.go but provides its own GetLinkType / ListOutgoingLinkTypes
// implementations that increment counters.
type countingLinkRepo struct {
	*mockRepo
	getLinkTypeCalls           int64
	listOutgoingLinkTypesCalls int64
}

func (c *countingLinkRepo) GetLinkType(ctx context.Context, rid string) (*oms.LinkType, error) {
	atomic.AddInt64(&c.getLinkTypeCalls, 1)
	return c.mockRepo.GetLinkType(ctx, rid)
}

func (c *countingLinkRepo) ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	atomic.AddInt64(&c.listOutgoingLinkTypesCalls, 1)
	return c.mockRepo.ListOutgoingLinkTypes(ctx, objectTypeRID)
}

func newCountingLinkRepo() *countingLinkRepo {
	return &countingLinkRepo{
		mockRepo: &mockRepo{
			linkTypes: map[string]*oms.LinkType{
				"ri.ontology.main.link-type.reports": {
					RID:              "ri.ontology.main.link-type.reports",
					APIName:          "reportsTo",
					Cardinality:      "ONE_TO_MANY",
					SourceObjectType: "ri.ontology.main.object-type.emp",
					TargetObjectType: "ri.ontology.main.object-type.emp",
				},
			},
			outgoing: map[string][]oms.LinkType{
				"ri.ontology.main.object-type.emp": {
					{
						RID:              "ri.ontology.main.link-type.reports",
						APIName:          "reportsTo",
						SourceObjectType: "ri.ontology.main.object-type.emp",
						TargetObjectType: "ri.ontology.main.object-type.emp",
					},
				},
			},
		},
	}
}

func TestLinkTypeCache_GetLinkType_CacheHit(t *testing.T) {
	cache := links.NewLinkTypeCache(60 * time.Second)
	base := newCountingLinkRepo()

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		lt, err := cache.GetLinkType(ctx, base, "ri.ontology.main.link-type.reports")
		if err != nil {
			t.Fatalf("GetLinkType: %v", err)
		}
		if lt.APIName != "reportsTo" {
			t.Fatalf("want reportsTo, got %q", lt.APIName)
		}
	}
	if got := atomic.LoadInt64(&base.getLinkTypeCalls); got != 1 {
		t.Fatalf("want 1 underlying call, got %d", got)
	}
}

func TestLinkTypeCache_ListOutgoing_CacheHit(t *testing.T) {
	cache := links.NewLinkTypeCache(60 * time.Second)
	base := newCountingLinkRepo()

	ctx := context.Background()
	for i := 0; i < 7; i++ {
		lts, err := cache.ListOutgoingLinkTypes(ctx, base, "ri.ontology.main.object-type.emp")
		if err != nil {
			t.Fatalf("ListOutgoingLinkTypes: %v", err)
		}
		if len(lts) != 1 {
			t.Fatalf("want 1 link type, got %d", len(lts))
		}
	}
	if got := atomic.LoadInt64(&base.listOutgoingLinkTypesCalls); got != 1 {
		t.Fatalf("want 1 underlying call, got %d", got)
	}
}

func TestLinkTypeCache_TTLExpiry(t *testing.T) {
	cache := links.NewLinkTypeCache(60 * time.Second)
	current := time.Unix(0, 0)
	cache.SetNowFunc(func() time.Time { return current })

	base := newCountingLinkRepo()
	ctx := context.Background()

	_, _ = cache.GetLinkType(ctx, base, "ri.ontology.main.link-type.reports")
	_, _ = cache.GetLinkType(ctx, base, "ri.ontology.main.link-type.reports")
	if got := atomic.LoadInt64(&base.getLinkTypeCalls); got != 1 {
		t.Fatalf("before expiry want 1, got %d", got)
	}

	current = current.Add(61 * time.Second)
	_, _ = cache.GetLinkType(ctx, base, "ri.ontology.main.link-type.reports")
	if got := atomic.LoadInt64(&base.getLinkTypeCalls); got != 2 {
		t.Fatalf("after expiry want 2, got %d", got)
	}
}

func TestLinkTypeCache_InvalidateAll(t *testing.T) {
	cache := links.NewLinkTypeCache(60 * time.Second)
	base := newCountingLinkRepo()
	ctx := context.Background()

	_, _ = cache.GetLinkType(ctx, base, "ri.ontology.main.link-type.reports")
	_, _ = cache.ListOutgoingLinkTypes(ctx, base, "ri.ontology.main.object-type.emp")

	cache.InvalidateAll()

	_, _ = cache.GetLinkType(ctx, base, "ri.ontology.main.link-type.reports")
	_, _ = cache.ListOutgoingLinkTypes(ctx, base, "ri.ontology.main.object-type.emp")

	if got := atomic.LoadInt64(&base.getLinkTypeCalls); got != 2 {
		t.Errorf("GetLinkType after InvalidateAll want 2, got %d", got)
	}
	if got := atomic.LoadInt64(&base.listOutgoingLinkTypesCalls); got != 2 {
		t.Errorf("ListOutgoing after InvalidateAll want 2, got %d", got)
	}
}

// Integration test: a Resolver configured with a LinkTypeCache should reuse
// cached link-type metadata across repeated ResolveLinkedObjectsByAPIName
// calls, avoiding repeat repo lookups.
func TestResolver_WithLinkTypeCache_ReusesMetadata(t *testing.T) {
	base := newCountingLinkRepo()
	base.objectTypes = map[string]*oms.ObjectType{
		"ri.ontology.main.object-type.emp": {
			RID:         "ri.ontology.main.object-type.emp",
			APIName:     "Employee",
			OntologyRID: "ri.ontology.main.ontology.n",
		},
	}

	resolver := links.NewResolver(base, nil)
	cache := links.NewLinkTypeCache(60 * time.Second)
	resolver.SetLinkTypeCache(cache)

	ctx := context.Background()

	// Call ResolveLinkedObjectsByAPIName multiple times. The first call primes
	// the outgoing-link cache; subsequent calls should not hit the repo.
	// We expect the call to error at the FK-resolution step (indexMgr is nil)
	// but the cache behavior is independent of that.
	for i := 0; i < 5; i++ {
		_, _ = resolver.ResolveLinkedObjectsByAPIName(ctx, "ri.ontology.main.object-type.emp", "reportsTo", []string{"1"})
	}
	if got := atomic.LoadInt64(&base.listOutgoingLinkTypesCalls); got != 1 {
		t.Fatalf("want 1 repo call to ListOutgoingLinkTypes (rest cache hits), got %d", got)
	}
}

// slowLinkRepo wraps countingLinkRepo with a configurable delay to simulate a
// real PostgreSQL round-trip (~50us) so the benchmarks reflect the advantage
// the cache brings in production.
type slowLinkRepo struct {
	*countingLinkRepo
	delay time.Duration
}

func (s *slowLinkRepo) GetLinkType(ctx context.Context, rid string) (*oms.LinkType, error) {
	time.Sleep(s.delay)
	return s.countingLinkRepo.GetLinkType(ctx, rid)
}

func (s *slowLinkRepo) ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	time.Sleep(s.delay)
	return s.countingLinkRepo.ListOutgoingLinkTypes(ctx, objectTypeRID)
}

func BenchmarkLinkTypeCache_GetLinkType(b *testing.B) {
	cache := links.NewLinkTypeCache(60 * time.Second)
	base := &slowLinkRepo{countingLinkRepo: newCountingLinkRepo(), delay: 50 * time.Microsecond}
	ctx := context.Background()

	_, _ = cache.GetLinkType(ctx, base, "ri.ontology.main.link-type.reports")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cache.GetLinkType(ctx, base, "ri.ontology.main.link-type.reports")
	}
}

func BenchmarkUncachedRepo_GetLinkType(b *testing.B) {
	base := &slowLinkRepo{countingLinkRepo: newCountingLinkRepo(), delay: 50 * time.Microsecond}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = base.GetLinkType(ctx, "ri.ontology.main.link-type.reports")
	}
}
