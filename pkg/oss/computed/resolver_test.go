package computed

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/liyang/weave/pkg/oms"
)

// --- Fakes ---

type fakeLinks struct {
	mu   sync.Mutex
	hits int
	byPK map[string]map[string][]string // linkRID -> sourcePK -> targetPKs
	err  error
}

func newFakeLinks() *fakeLinks {
	return &fakeLinks{byPK: map[string]map[string][]string{}}
}

func (f *fakeLinks) ResolveLinkedObjects(_ context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.hits++
	out := []string{}
	for _, pk := range sourcePKs {
		out = append(out, f.byPK[linkTypeRID][pk]...)
	}
	return out, nil
}

func (f *fakeLinks) setLink(linkRID, sourcePK string, targets []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byPK[linkRID]; !ok {
		f.byPK[linkRID] = map[string][]string{}
	}
	f.byPK[linkRID][sourcePK] = targets
}

type fakeLookup struct {
	links map[string]*oms.LinkType
	err   error
}

func newFakeLookup() *fakeLookup {
	return &fakeLookup{links: map[string]*oms.LinkType{}}
}

func (f *fakeLookup) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	if f.err != nil {
		return nil, f.err
	}
	lt, ok := f.links[rid]
	if !ok {
		return nil, errors.New("not found")
	}
	return lt, nil
}

// indexSearcher wraps a map of objectType -> bleve.Index so tests can drive
// multi-type lookups through the Resolver's IndexSearcher contract.
type indexSearcher struct {
	idxs map[string]bleve.Index
}

func (s *indexSearcher) Search(objectType string, req *bleve.SearchRequest) (*bleve.SearchResult, error) {
	idx, ok := s.idxs[objectType]
	if !ok {
		return nil, errors.New("index not found for " + objectType)
	}
	return idx.Search(req)
}

func newOrderIndex(t *testing.T, docs map[string]map[string]interface{}) bleve.Index {
	t.Helper()
	indexMapping := bleve.NewIndexMapping()
	doc := bleve.NewDocumentMapping()
	doc.AddFieldMappingsAt("amount", mapping.NewNumericFieldMapping())
	doc.AddFieldMappingsAt("quantity", mapping.NewNumericFieldMapping())
	doc.AddFieldMappingsAt("label", mapping.NewTextFieldMapping())
	indexMapping.DefaultMapping = doc

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "orders"), indexMapping)
	if err != nil {
		t.Fatalf("bleve.New: %v", err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	for id, fields := range docs {
		if err := idx.Index(id, fields); err != nil {
			t.Fatalf("index %q: %v", id, err)
		}
	}
	return idx
}

// Fixture shared by most tests: a customer with three orders via link
// ri.link.customer-orders; target ObjectType "order" has amount + quantity.
type fixture struct {
	resolver *Resolver
	links    *fakeLinks
	lookup   *fakeLookup
	idx      bleve.Index
	cp       *oms.ComputedProperty
	now      time.Time
	nowMu    sync.Mutex
}

func setupFixture(t *testing.T) *fixture {
	t.Helper()
	fx := &fixture{}
	fx.links = newFakeLinks()
	fx.links.setLink("ri.link.customer-orders", "c1", []string{"o1", "o2", "o3"})
	fx.links.setLink("ri.link.customer-orders", "c2", []string{})

	fx.lookup = newFakeLookup()
	fx.lookup.links["ri.link.customer-orders"] = &oms.LinkType{
		RID:              "ri.link.customer-orders",
		SourceObjectType: "customer",
		TargetObjectType: "order",
		Cardinality:      "ONE_TO_MANY",
	}

	fx.idx = newOrderIndex(t, map[string]map[string]interface{}{
		"o1": {"amount": 100.0, "quantity": 2.0, "label": "a"},
		"o2": {"amount": 50.0, "quantity": 1.0, "label": "b"},
		"o3": {"amount": 25.0, "quantity": 5.0, "label": "c"},
	})

	fx.resolver = NewResolver(fx.links, fx.lookup, &indexSearcher{
		idxs: map[string]bleve.Index{"order": fx.idx},
	})

	fx.now = time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	fx.resolver.SetNowFunc(func() time.Time {
		fx.nowMu.Lock()
		defer fx.nowMu.Unlock()
		return fx.now
	})

	fx.cp = &oms.ComputedProperty{
		RID:             "ri.ontology.main.computed-property.order-count",
		ObjectTypeRID:   "ri.ontology.main.object-type.customer",
		APIName:         "orderCount",
		SourceLinkRID:   "ri.link.customer-orders",
		Aggregation:     oms.ComputedAggregation{Type: "count"},
		CacheTTLSeconds: 60,
	}
	return fx
}

func (fx *fixture) advanceBy(d time.Duration) {
	fx.nowMu.Lock()
	defer fx.nowMu.Unlock()
	fx.now = fx.now.Add(d)
}

// --- Tests ---

func TestResolver_Count(t *testing.T) {
	fx := setupFixture(t)

	v, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got, want := v.Value, int64(3); got != want {
		t.Errorf("count = %v, want %v", got, want)
	}
	if v.ComputedAt.IsZero() {
		t.Errorf("ComputedAt should be non-zero")
	}
}

func TestResolver_CountZero(t *testing.T) {
	fx := setupFixture(t)

	v, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c2")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got, want := v.Value, int64(0); got != want {
		t.Errorf("count = %v, want %v", got, want)
	}
}

func TestResolver_Sum(t *testing.T) {
	fx := setupFixture(t)
	fx.cp.Aggregation = oms.ComputedAggregation{Type: "sum", Field: "amount"}

	v, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got, want := v.Value.(float64), 175.0; got != want {
		t.Errorf("sum = %v, want %v", got, want)
	}
}

func TestResolver_Avg(t *testing.T) {
	fx := setupFixture(t)
	fx.cp.Aggregation = oms.ComputedAggregation{Type: "avg", Field: "amount"}

	v, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got := v.Value.(float64)
	want := 175.0 / 3.0
	if diff := got - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("avg = %v, want %v", got, want)
	}
}

func TestResolver_Min(t *testing.T) {
	fx := setupFixture(t)
	fx.cp.Aggregation = oms.ComputedAggregation{Type: "min", Field: "amount"}

	v, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got, want := v.Value.(float64), 25.0; got != want {
		t.Errorf("min = %v, want %v", got, want)
	}
}

func TestResolver_Max(t *testing.T) {
	fx := setupFixture(t)
	fx.cp.Aggregation = oms.ComputedAggregation{Type: "max", Field: "amount"}

	v, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got, want := v.Value.(float64), 100.0; got != want {
		t.Errorf("max = %v, want %v", got, want)
	}
}

func TestResolver_SumEmpty(t *testing.T) {
	fx := setupFixture(t)
	fx.cp.Aggregation = oms.ComputedAggregation{Type: "sum", Field: "amount"}

	v, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c2")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if got, want := v.Value.(float64), 0.0; got != want {
		t.Errorf("sum(empty) = %v, want %v", got, want)
	}
}

func TestResolver_AvgEmpty(t *testing.T) {
	fx := setupFixture(t)
	fx.cp.Aggregation = oms.ComputedAggregation{Type: "avg", Field: "amount"}

	v, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c2")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if v.Value != nil {
		t.Errorf("avg(empty) = %v, want nil", v.Value)
	}
}

// Cache behavior — US-202: 查询时 lazy 触发聚合，默认 60s TTL 缓存

func TestResolver_CacheHitWithinTTL(t *testing.T) {
	fx := setupFixture(t)

	first, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	linkHitsAfterFirst := fx.links.hits

	// Mutate the underlying link set. A cache hit should return the *old*
	// value.
	fx.links.setLink("ri.link.customer-orders", "c1", []string{"o1"})

	fx.advanceBy(30 * time.Second)
	second, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}

	if got, want := second.Value, first.Value; got != want {
		t.Errorf("second Value = %v, want cached %v", got, want)
	}
	if !second.ComputedAt.Equal(first.ComputedAt) {
		t.Errorf("second ComputedAt = %v, want %v (cache should preserve)", second.ComputedAt, first.ComputedAt)
	}
	if fx.links.hits != linkHitsAfterFirst {
		t.Errorf("link resolver hit %d times after cache hit, want %d", fx.links.hits, linkHitsAfterFirst)
	}
}

func TestResolver_CacheExpiresAfterTTL(t *testing.T) {
	fx := setupFixture(t)

	first, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("first Evaluate: %v", err)
	}
	linkHitsAfterFirst := fx.links.hits

	fx.links.setLink("ri.link.customer-orders", "c1", []string{"o1"})
	fx.advanceBy(61 * time.Second)

	second, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("second Evaluate: %v", err)
	}

	if got, want := second.Value, int64(1); got != want {
		t.Errorf("after TTL expiry Value = %v, want %v (recomputed)", got, want)
	}
	if !second.ComputedAt.After(first.ComputedAt) {
		t.Errorf("after expiry ComputedAt should advance past %v, got %v", first.ComputedAt, second.ComputedAt)
	}
	if fx.links.hits != linkHitsAfterFirst+1 {
		t.Errorf("expected one fresh link resolve after TTL, got hits=%d", fx.links.hits)
	}
}

func TestResolver_DefaultTTLWhenZero(t *testing.T) {
	fx := setupFixture(t)
	fx.cp.CacheTTLSeconds = 0 // exercise the default

	if _, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	hitsBefore := fx.links.hits

	// 59s later: should still be cached under the default 60s TTL.
	fx.advanceBy(59 * time.Second)
	if _, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1"); err != nil {
		t.Fatalf("second: %v", err)
	}
	if fx.links.hits != hitsBefore {
		t.Errorf("default TTL did not protect cache: hits=%d want=%d", fx.links.hits, hitsBefore)
	}
}

func TestResolver_CacheKeyedByPK(t *testing.T) {
	fx := setupFixture(t)

	fx.links.setLink("ri.link.customer-orders", "c1", []string{"o1", "o2"})
	fx.links.setLink("ri.link.customer-orders", "c3", []string{"o3"})

	v1, _ := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	v3, _ := fx.resolver.Evaluate(context.Background(), fx.cp, "c3")

	if got, want := v1.Value, int64(2); got != want {
		t.Errorf("c1 = %v want %v", got, want)
	}
	if got, want := v3.Value, int64(1); got != want {
		t.Errorf("c3 = %v want %v", got, want)
	}
}

func TestResolver_CacheKeyedByComputedPropertyRID(t *testing.T) {
	fx := setupFixture(t)

	cp2 := *fx.cp
	cp2.RID = "ri.ontology.main.computed-property.revenue"
	cp2.APIName = "revenue"
	cp2.Aggregation = oms.ComputedAggregation{Type: "sum", Field: "amount"}

	count, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	revenue, err := fx.resolver.Evaluate(context.Background(), &cp2, "c1")
	if err != nil {
		t.Fatalf("revenue: %v", err)
	}
	if got, want := count.Value, int64(3); got != want {
		t.Errorf("count = %v want %v", got, want)
	}
	if got, want := revenue.Value.(float64), 175.0; got != want {
		t.Errorf("revenue = %v want %v", got, want)
	}
}

func TestResolver_InvalidateAll(t *testing.T) {
	fx := setupFixture(t)

	if _, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	fx.links.setLink("ri.link.customer-orders", "c1", []string{"o1"})
	fx.resolver.InvalidateAll()

	v, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	if err != nil {
		t.Fatalf("post-invalidate: %v", err)
	}
	if got, want := v.Value, int64(1); got != want {
		t.Errorf("after invalidate = %v want %v", got, want)
	}
}

func TestResolver_NoCacheWhenTTLNegative(t *testing.T) {
	fx := setupFixture(t)
	fx.cp.CacheTTLSeconds = -1 // opt out of caching entirely

	if _, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1"); err != nil {
		t.Fatalf("first: %v", err)
	}
	hitsBefore := fx.links.hits

	if _, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1"); err != nil {
		t.Fatalf("second: %v", err)
	}
	if fx.links.hits != hitsBefore+1 {
		t.Errorf("expected fresh compute every call when TTL<=0, hits=%d wanted=%d", fx.links.hits, hitsBefore+1)
	}
}

func TestResolver_InvalidAggregationRejected(t *testing.T) {
	fx := setupFixture(t)
	fx.cp.Aggregation = oms.ComputedAggregation{Type: "median", Field: "amount"}

	if _, err := fx.resolver.Evaluate(context.Background(), fx.cp, "c1"); err == nil {
		t.Fatalf("expected error for unknown aggregation type")
	}
}

func TestResolver_ComputedAtMonotonic(t *testing.T) {
	fx := setupFixture(t)

	v1, _ := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")
	fx.advanceBy(120 * time.Second)
	v2, _ := fx.resolver.Evaluate(context.Background(), fx.cp, "c1")

	if !v2.ComputedAt.After(v1.ComputedAt) {
		t.Errorf("v2.ComputedAt (%v) should be after v1 (%v)", v2.ComputedAt, v1.ComputedAt)
	}
}
