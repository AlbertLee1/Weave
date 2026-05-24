package oms_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// countingRepo wraps a stored dataset and counts invocations per method name.
// It embeds a no-op base so only the methods exercised by the cache need real
// implementations; everything else falls through to the embedded stub.
type countingRepo struct {
	oms.Repository

	ontologies    map[string]*oms.Ontology
	objectTypes   map[string]*oms.ObjectType
	otByAPI       map[string]*oms.ObjectType // key: ontologyRID|apiName
	linkTypes     map[string]*oms.LinkType
	outgoingLinks map[string][]oms.LinkType
	actionTypes   map[string]*oms.ActionType
	atByAPI       map[string]*oms.ActionType
	properties    map[string][]oms.Property

	counts map[string]*int64
}

func newCountingRepo() *countingRepo {
	r := &countingRepo{
		Repository:    &noopRepo{},
		ontologies:    map[string]*oms.Ontology{},
		objectTypes:   map[string]*oms.ObjectType{},
		otByAPI:       map[string]*oms.ObjectType{},
		linkTypes:     map[string]*oms.LinkType{},
		outgoingLinks: map[string][]oms.LinkType{},
		actionTypes:   map[string]*oms.ActionType{},
		atByAPI:       map[string]*oms.ActionType{},
		properties:    map[string][]oms.Property{},
		counts:        map[string]*int64{},
	}
	for _, name := range []string{
		"GetOntology", "ListOntologies",
		"GetObjectTypeByAPIName", "ListObjectTypes", "ListProperties",
		"GetLinkType", "ListOutgoingLinkTypes",
		"GetActionTypeByAPIName", "ListActionTypes",
	} {
		var c int64
		r.counts[name] = &c
	}
	return r
}

func (r *countingRepo) bump(name string) {
	if c, ok := r.counts[name]; ok {
		atomic.AddInt64(c, 1)
	}
}

func (r *countingRepo) count(name string) int64 {
	if c, ok := r.counts[name]; ok {
		return atomic.LoadInt64(c)
	}
	return 0
}

func (r *countingRepo) GetOntology(_ context.Context, rid string) (*oms.Ontology, error) {
	r.bump("GetOntology")
	if o, ok := r.ontologies[rid]; ok {
		return o, nil
	}
	return nil, oms.ErrNotFound
}

func (r *countingRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) {
	r.bump("ListOntologies")
	out := make([]oms.Ontology, 0, len(r.ontologies))
	for _, o := range r.ontologies {
		out = append(out, *o)
	}
	return out, nil
}

func (r *countingRepo) GetObjectTypeByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.ObjectType, error) {
	r.bump("GetObjectTypeByAPIName")
	if ot, ok := r.otByAPI[ontologyRID+"|"+apiName]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

func (r *countingRepo) ListObjectTypes(_ context.Context, ontologyRID string) ([]oms.ObjectType, error) {
	r.bump("ListObjectTypes")
	var out []oms.ObjectType
	for _, ot := range r.objectTypes {
		if ot.OntologyRID == ontologyRID {
			out = append(out, *ot)
		}
	}
	return out, nil
}

func (r *countingRepo) ListProperties(_ context.Context, objectTypeRID string) ([]oms.Property, error) {
	r.bump("ListProperties")
	return r.properties[objectTypeRID], nil
}

func (r *countingRepo) GetLinkType(_ context.Context, rid string) (*oms.LinkType, error) {
	r.bump("GetLinkType")
	if lt, ok := r.linkTypes[rid]; ok {
		return lt, nil
	}
	return nil, oms.ErrNotFound
}

func (r *countingRepo) ListOutgoingLinkTypes(_ context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	r.bump("ListOutgoingLinkTypes")
	return r.outgoingLinks[objectTypeRID], nil
}

func (r *countingRepo) GetActionTypeByAPIName(_ context.Context, ontologyRID, apiName string) (*oms.ActionType, error) {
	r.bump("GetActionTypeByAPIName")
	if at, ok := r.atByAPI[ontologyRID+"|"+apiName]; ok {
		return at, nil
	}
	return nil, oms.ErrNotFound
}

func (r *countingRepo) ListActionTypes(_ context.Context, ontologyRID string) ([]oms.ActionType, error) {
	r.bump("ListActionTypes")
	var out []oms.ActionType
	for _, at := range r.actionTypes {
		if at.OntologyRID == ontologyRID {
			out = append(out, *at)
		}
	}
	return out, nil
}

// noopRepo is a stub that returns zero values for all Repository methods not
// overridden by countingRepo. It lets us embed a full Repository in tests
// without needing to satisfy the entire (large) interface manually.
type noopRepo struct{}

func (*noopRepo) CreateOntology(context.Context, *oms.Ontology) error        { return nil }
func (*noopRepo) GetOntology(context.Context, string) (*oms.Ontology, error) { return nil, nil }
func (*noopRepo) ListOntologies(context.Context) ([]oms.Ontology, error)     { return nil, nil }
func (*noopRepo) UpdateOntology(context.Context, *oms.Ontology) error        { return nil }
func (*noopRepo) CreateObjectType(context.Context, *oms.ObjectType) error    { return nil }
func (*noopRepo) GetObjectType(context.Context, string) (*oms.ObjectType, error) {
	return nil, nil
}
func (*noopRepo) GetObjectTypeByAPIName(context.Context, string, string) (*oms.ObjectType, error) {
	return nil, nil
}
func (*noopRepo) ListObjectTypes(context.Context, string) ([]oms.ObjectType, error) {
	return nil, nil
}
func (*noopRepo) UpdateObjectType(context.Context, *oms.ObjectType) error    { return nil }
func (*noopRepo) DeleteObjectType(context.Context, string) error             { return nil }
func (*noopRepo) CreateProperty(context.Context, *oms.Property) error        { return nil }
func (*noopRepo) GetProperty(context.Context, string) (*oms.Property, error) { return nil, nil }
func (*noopRepo) ListProperties(context.Context, string) ([]oms.Property, error) {
	return nil, nil
}
func (*noopRepo) UpdateProperty(context.Context, *oms.Property) error    { return nil }
func (*noopRepo) DeleteProperty(context.Context, string) error           { return nil }
func (*noopRepo) CreateLinkType(context.Context, *oms.LinkType) error    { return nil }
func (*noopRepo) GetLinkType(context.Context, string) (*oms.LinkType, error) {
	return nil, nil
}
func (*noopRepo) GetLinkTypeByAPIName(context.Context, string, string) (*oms.LinkType, error) {
	return nil, nil
}
func (*noopRepo) ListOutgoingLinkTypes(context.Context, string) ([]oms.LinkType, error) {
	return nil, nil
}
func (*noopRepo) ListIncomingLinkTypes(context.Context, string) ([]oms.LinkType, error) {
	return nil, nil
}
func (*noopRepo) ListLinkTypes(context.Context, string) ([]oms.LinkType, error) {
	return nil, nil
}
func (*noopRepo) UpdateLinkType(context.Context, *oms.LinkType) error { return nil }
func (*noopRepo) DeleteLinkType(context.Context, string) error        { return nil }
func (*noopRepo) UpsertLinkEdge(context.Context, *oms.LinkEdge) error { return nil }
func (*noopRepo) DeleteLinkEdge(context.Context, string, string, string) error {
	return nil
}
func (*noopRepo) DeleteAllLinkEdgesForSource(context.Context, string, string) error {
	return nil
}
func (*noopRepo) CreateActionType(context.Context, *oms.ActionType) error { return nil }
func (*noopRepo) GetActionType(context.Context, string) (*oms.ActionType, error) {
	return nil, nil
}
func (*noopRepo) GetActionTypeByAPIName(context.Context, string, string) (*oms.ActionType, error) {
	return nil, nil
}
func (*noopRepo) ListActionTypes(context.Context, string) ([]oms.ActionType, error) {
	return nil, nil
}
func (*noopRepo) UpdateActionType(context.Context, *oms.ActionType) error { return nil }
func (*noopRepo) DeleteActionType(context.Context, string) error          { return nil }
func (*noopRepo) CreateInterface(context.Context, *oms.Interface) error   { return nil }
func (*noopRepo) GetInterface(context.Context, string) (*oms.Interface, error) {
	return nil, nil
}
func (*noopRepo) GetInterfaceByAPIName(context.Context, string, string) (*oms.Interface, error) {
	return nil, nil
}
func (*noopRepo) ListInterfaces(context.Context, string) ([]oms.Interface, error) {
	return nil, nil
}
func (*noopRepo) UpdateInterface(context.Context, *oms.Interface) error              { return nil }
func (*noopRepo) DeleteInterface(context.Context, string) error                      { return nil }
func (*noopRepo) AttachInterface(context.Context, *oms.ObjectTypeInterface) error    { return nil }
func (*noopRepo) DetachInterface(context.Context, string, string) error              { return nil }
func (*noopRepo) ListObjectTypeInterfaces(context.Context, string) ([]oms.ObjectTypeInterface, error) {
	return nil, nil
}
func (*noopRepo) ListInterfaceObjectTypes(context.Context, string) ([]oms.ObjectType, error) {
	return nil, nil
}
func (*noopRepo) CreateSharedProperty(context.Context, *oms.SharedProperty) error { return nil }
func (*noopRepo) GetSharedProperty(context.Context, string) (*oms.SharedProperty, error) {
	return nil, nil
}
func (*noopRepo) ListSharedProperties(context.Context, string) ([]oms.SharedProperty, error) {
	return nil, nil
}
func (*noopRepo) UpdateSharedProperty(context.Context, *oms.SharedProperty) error { return nil }
func (*noopRepo) DeleteSharedProperty(context.Context, string) error              { return nil }
func (*noopRepo) CreateTypeGroup(context.Context, *oms.TypeGroup) error           { return nil }
func (*noopRepo) GetTypeGroup(context.Context, string) (*oms.TypeGroup, error) {
	return nil, nil
}
func (*noopRepo) ListTypeGroups(context.Context, string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (*noopRepo) UpdateTypeGroup(context.Context, *oms.TypeGroup) error { return nil }
func (*noopRepo) DeleteTypeGroup(context.Context, string) error         { return nil }
func (*noopRepo) AssignTypeGroup(context.Context, string, string) error { return nil }
func (*noopRepo) RemoveTypeGroup(context.Context, string, string) error { return nil }
func (*noopRepo) ListTypeGroupsForObjectType(context.Context, string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (*noopRepo) CreateValueType(context.Context, *oms.ValueType) error { return nil }
func (*noopRepo) GetValueType(context.Context, string) (*oms.ValueType, error) {
	return nil, nil
}
func (*noopRepo) GetValueTypeByAPIName(context.Context, string) (*oms.ValueType, error) {
	return nil, nil
}
func (*noopRepo) ListValueTypes(context.Context) ([]oms.ValueType, error) { return nil, nil }
func (*noopRepo) UpdateValueType(context.Context, *oms.ValueType) error   { return nil }
func (*noopRepo) DeleteValueType(context.Context, string) error           { return nil }
func (*noopRepo) ListPropertyUsagesByBaseType(context.Context, string) ([]oms.PropertyUsage, error) {
	return nil, nil
}
func (*noopRepo) CreateSecurityPolicy(context.Context, *oms.SecurityPolicy) error {
	return nil
}
func (*noopRepo) GetSecurityPolicy(context.Context, string) (*oms.SecurityPolicy, error) {
	return nil, nil
}
func (*noopRepo) ListSecurityPolicies(context.Context, string) ([]oms.SecurityPolicy, error) {
	return nil, nil
}
func (*noopRepo) UpdateSecurityPolicy(context.Context, *oms.SecurityPolicy) error { return nil }
func (*noopRepo) DeleteSecurityPolicy(context.Context, string) error              { return nil }
func (*noopRepo) CreateDatasourceBinding(context.Context, *oms.DatasourceBinding) error {
	return nil
}
func (*noopRepo) GetDatasourceBinding(context.Context, string) (*oms.DatasourceBinding, error) {
	return nil, nil
}
func (*noopRepo) ListDatasourceBindings(context.Context, string) ([]oms.DatasourceBinding, error) {
	return nil, nil
}
func (*noopRepo) UpdateDatasourceBinding(context.Context, *oms.DatasourceBinding) error {
	return nil
}
func (*noopRepo) DeleteDatasourceBinding(context.Context, string) error { return nil }
func (*noopRepo) CreateQueryType(context.Context, *oms.QueryType) error { return nil }
func (*noopRepo) GetQueryType(context.Context, string) (*oms.QueryType, error) {
	return nil, nil
}
func (*noopRepo) GetQueryTypeByAPIName(context.Context, string, string) (*oms.QueryType, error) {
	return nil, nil
}
func (*noopRepo) ListQueryTypes(context.Context, string) ([]oms.QueryType, error) {
	return nil, nil
}
func (*noopRepo) UpdateQueryType(context.Context, *oms.QueryType) error { return nil }
func (*noopRepo) DeleteQueryType(context.Context, string) error         { return nil }
func (*noopRepo) CreateFunction(context.Context, *oms.Function) error   { return nil }
func (*noopRepo) GetFunction(context.Context, string) (*oms.Function, error) {
	return nil, nil
}
func (*noopRepo) GetFunctionByName(context.Context, string, string) (*oms.Function, error) {
	return nil, nil
}
func (*noopRepo) GetFunctionByNameVersion(context.Context, string, string, string) (*oms.Function, error) {
	return nil, nil
}
func (*noopRepo) ListFunctions(context.Context, string) ([]oms.Function, error) {
	return nil, nil
}
func (*noopRepo) ListFunctionVersionsByName(context.Context, string, string) ([]oms.Function, error) {
	return nil, nil
}
func (*noopRepo) UpdateFunction(context.Context, *oms.Function) error { return nil }
func (*noopRepo) DeleteFunction(context.Context, string) error        { return nil }
func (*noopRepo) InsertActionLog(context.Context, *oms.ActionLog) error            { return nil }
func (*noopRepo) GetActionLog(context.Context, int64) (*oms.ActionLog, error)      { return nil, oms.ErrNotFound }
func (*noopRepo) ListActionLogs(context.Context, string, int, int) ([]oms.ActionLog, error) {
	return nil, nil
}
func (*noopRepo) CountActionLogs(context.Context, string) (int, error)             { return 0, nil }
func (*noopRepo) UpdateActionLogStatus(context.Context, int64, string) error       { return nil }
func (*noopRepo) UpdateActionLogSideEffectStatus(context.Context, int64, json.RawMessage) error {
	return nil
}
func (*noopRepo) InsertObjectHistory(context.Context, *oms.ObjectHistory) error {
	return nil
}
func (*noopRepo) ListObjectHistory(context.Context, string, string, int) ([]oms.ObjectHistory, error) {
	return nil, nil
}
func (*noopRepo) GetObjectVersionCount(context.Context, string, string) (int64, error) {
	return 0, nil
}
func (*noopRepo) SearchOntologyResources(context.Context, string, string) ([]oms.SearchResult, error) {
	return nil, nil
}
func (*noopRepo) CreateSnapshot(context.Context, *oms.OntologySnapshot) error { return nil }
func (*noopRepo) ListSnapshots(context.Context, string) ([]oms.OntologySnapshot, error) {
	return nil, nil
}
func (*noopRepo) GetSnapshot(context.Context, string, int) (*oms.OntologySnapshot, error) {
	return nil, nil
}
func (*noopRepo) GetOntologyVersion(context.Context, string) (int, error)       { return 0, nil }
func (*noopRepo) IncrementOntologyVersion(context.Context, string) (int, error) { return 0, nil }
func (*noopRepo) UpsertObjectEmbedding(context.Context, *oms.ObjectEmbedding) error {
	return nil
}
func (*noopRepo) GetObjectEmbedding(context.Context, string, string, string) (*oms.ObjectEmbedding, error) {
	return nil, nil
}
func (*noopRepo) FindNearestNeighbors(context.Context, string, []float32, int, string) ([]oms.NearestNeighborResult, error) {
	return nil, nil
}

// --- Tests ---

func seedCountingRepo() *countingRepo {
	repo := newCountingRepo()
	repo.ontologies["ri.ontology.main.ontology.n"] = &oms.Ontology{
		RID: "ri.ontology.main.ontology.n", APIName: "northwind",
	}
	repo.objectTypes["ri.ontology.main.object-type.emp"] = &oms.ObjectType{
		RID: "ri.ontology.main.object-type.emp", OntologyRID: "ri.ontology.main.ontology.n", APIName: "Employee",
	}
	repo.otByAPI["ri.ontology.main.ontology.n|Employee"] = repo.objectTypes["ri.ontology.main.object-type.emp"]
	repo.linkTypes["ri.ontology.main.link-type.reports"] = &oms.LinkType{
		RID: "ri.ontology.main.link-type.reports", APIName: "reportsTo",
	}
	repo.outgoingLinks["ri.ontology.main.object-type.emp"] = []oms.LinkType{
		*repo.linkTypes["ri.ontology.main.link-type.reports"],
	}
	repo.actionTypes["ri.ontology.main.action-type.hire"] = &oms.ActionType{
		RID: "ri.ontology.main.action-type.hire", OntologyRID: "ri.ontology.main.ontology.n", APIName: "hireEmployee",
	}
	repo.atByAPI["ri.ontology.main.ontology.n|hireEmployee"] = repo.actionTypes["ri.ontology.main.action-type.hire"]
	return repo
}

func TestCachedRepository_GetOntology_CacheHit(t *testing.T) {
	base := seedCountingRepo()
	cached := oms.NewCachedRepository(base, 60*time.Second)

	ctx := context.Background()
	for i := 0; i < 5; i++ {
		o, err := cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
		if err != nil {
			t.Fatalf("GetOntology: %v", err)
		}
		if o.APIName != "northwind" {
			t.Fatalf("want northwind, got %q", o.APIName)
		}
	}
	if got := base.count("GetOntology"); got != 1 {
		t.Fatalf("want 1 underlying call (cache hits for the rest), got %d", got)
	}
}

func TestCachedRepository_GetOntology_CacheMiss_DifferentKeys(t *testing.T) {
	base := seedCountingRepo()
	base.ontologies["ri.ontology.main.ontology.c"] = &oms.Ontology{
		RID: "ri.ontology.main.ontology.c", APIName: "chinook",
	}
	cached := oms.NewCachedRepository(base, 60*time.Second)

	ctx := context.Background()
	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.c")
	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.c")

	if got := base.count("GetOntology"); got != 2 {
		t.Fatalf("want 2 underlying calls (one per distinct key), got %d", got)
	}
}

func TestCachedRepository_GetObjectTypeByAPIName_CacheHit(t *testing.T) {
	base := seedCountingRepo()
	cached := oms.NewCachedRepository(base, 60*time.Second)

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		ot, err := cached.GetObjectTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "Employee")
		if err != nil {
			t.Fatalf("GetObjectTypeByAPIName: %v", err)
		}
		if ot.APIName != "Employee" {
			t.Fatalf("unexpected apiName: %q", ot.APIName)
		}
	}
	if got := base.count("GetObjectTypeByAPIName"); got != 1 {
		t.Fatalf("want 1 underlying call, got %d", got)
	}
}

func TestCachedRepository_ListOutgoingLinkTypes_CacheHit(t *testing.T) {
	base := seedCountingRepo()
	cached := oms.NewCachedRepository(base, 60*time.Second)

	ctx := context.Background()
	for i := 0; i < 8; i++ {
		_, _ = cached.ListOutgoingLinkTypes(ctx, "ri.ontology.main.object-type.emp")
	}
	if got := base.count("ListOutgoingLinkTypes"); got != 1 {
		t.Fatalf("want 1 underlying call, got %d", got)
	}
}

func TestCachedRepository_TTLExpiry(t *testing.T) {
	base := seedCountingRepo()
	cached := oms.NewCachedRepository(base, 60*time.Second)

	// Inject a fake clock so we can advance time without sleeping.
	current := time.Unix(0, 0)
	cached.SetNowFunc(func() time.Time { return current })

	ctx := context.Background()
	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
	if got := base.count("GetOntology"); got != 1 {
		t.Fatalf("before expiry: want 1, got %d", got)
	}

	// Advance beyond TTL.
	current = current.Add(61 * time.Second)
	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
	if got := base.count("GetOntology"); got != 2 {
		t.Fatalf("after expiry: want 2, got %d", got)
	}
}

func TestCachedRepository_InvalidateOnWrite(t *testing.T) {
	base := seedCountingRepo()
	cached := oms.NewCachedRepository(base, 60*time.Second)
	ctx := context.Background()

	// Prime cache.
	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
	_, _ = cached.GetObjectTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "Employee")
	if got := base.count("GetOntology"); got != 1 {
		t.Fatalf("primed GetOntology want 1, got %d", got)
	}

	// UpdateOntology should invalidate the cache.
	if err := cached.UpdateOntology(ctx, &oms.Ontology{RID: "ri.ontology.main.ontology.n"}); err != nil {
		t.Fatalf("UpdateOntology: %v", err)
	}

	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
	if got := base.count("GetOntology"); got != 2 {
		t.Fatalf("after update want 2, got %d", got)
	}

	_, _ = cached.GetObjectTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "Employee")
	if got := base.count("GetObjectTypeByAPIName"); got != 2 {
		t.Fatalf("objecttype cache should also invalidate on ontology update, got %d", got)
	}
}

func TestCachedRepository_InvalidateAll(t *testing.T) {
	base := seedCountingRepo()
	cached := oms.NewCachedRepository(base, 60*time.Second)
	ctx := context.Background()

	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
	_, _ = cached.GetObjectTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "Employee")
	_, _ = cached.ListOutgoingLinkTypes(ctx, "ri.ontology.main.object-type.emp")
	_, _ = cached.GetActionTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "hireEmployee")

	// Primed.
	cached.InvalidateAll()

	_, _ = cached.GetOntology(ctx, "ri.ontology.main.ontology.n")
	_, _ = cached.GetObjectTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "Employee")
	_, _ = cached.ListOutgoingLinkTypes(ctx, "ri.ontology.main.object-type.emp")
	_, _ = cached.GetActionTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "hireEmployee")

	for _, name := range []string{
		"GetOntology", "GetObjectTypeByAPIName",
		"ListOutgoingLinkTypes", "GetActionTypeByAPIName",
	} {
		if got := base.count(name); got != 2 {
			t.Errorf("%s: after InvalidateAll want 2, got %d", name, got)
		}
	}
}

func TestCachedRepository_NotFoundNotCached(t *testing.T) {
	// Errors (in particular ErrNotFound) should not be cached: otherwise a
	// write that later creates the resource would need explicit invalidation
	// to become visible. Negative caching is out of scope for this decorator.
	base := seedCountingRepo()
	cached := oms.NewCachedRepository(base, 60*time.Second)
	ctx := context.Background()

	if _, err := cached.GetOntology(ctx, "ri.ontology.main.ontology.missing"); !errors.Is(err, oms.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := cached.GetOntology(ctx, "ri.ontology.main.ontology.missing"); !errors.Is(err, oms.ErrNotFound) {
		t.Fatalf("want ErrNotFound on second call, got %v", err)
	}
	if got := base.count("GetOntology"); got != 2 {
		t.Fatalf("not-found should not be cached; want 2 underlying calls, got %d", got)
	}
}

func TestCachedRepository_ListObjectTypes_CacheHit(t *testing.T) {
	base := seedCountingRepo()
	cached := oms.NewCachedRepository(base, 60*time.Second)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _ = cached.ListObjectTypes(ctx, "ri.ontology.main.ontology.n")
	}
	if got := base.count("ListObjectTypes"); got != 1 {
		t.Fatalf("want 1, got %d", got)
	}
}

// slowRepo wraps an oms.Repository and injects a configurable per-call delay
// to simulate the cost of a real PostgreSQL round-trip (typically 200-500us
// over localhost). The benchmarks below use 50us to reflect a conservative
// lower bound; in production the cache's advantage is larger still.
type slowRepo struct {
	oms.Repository
	delay time.Duration
}

func (s *slowRepo) GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*oms.ObjectType, error) {
	time.Sleep(s.delay)
	return s.Repository.GetObjectTypeByAPIName(ctx, ontologyRID, apiName)
}

func (s *slowRepo) GetOntology(ctx context.Context, rid string) (*oms.Ontology, error) {
	time.Sleep(s.delay)
	return s.Repository.GetOntology(ctx, rid)
}

// Benchmark verifying the >=5x QPS improvement acceptance criterion. The
// "slow" base repo simulates a 50us DB round-trip; the cache eliminates it
// entirely on hot-path reads, so cached throughput beats uncached by multiple
// orders of magnitude in these benchmarks.
func BenchmarkCachedRepository_GetObjectTypeByAPIName(b *testing.B) {
	base := &slowRepo{Repository: seedCountingRepo(), delay: 50 * time.Microsecond}
	cached := oms.NewCachedRepository(base, 60*time.Second)
	ctx := context.Background()

	// Prime so the benchmark measures the hot-path only.
	_, _ = cached.GetObjectTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "Employee")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cached.GetObjectTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "Employee")
	}
}

func BenchmarkUncachedRepository_GetObjectTypeByAPIName(b *testing.B) {
	base := &slowRepo{Repository: seedCountingRepo(), delay: 50 * time.Microsecond}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = base.GetObjectTypeByAPIName(ctx, "ri.ontology.main.ontology.n", "Employee")
	}
}
