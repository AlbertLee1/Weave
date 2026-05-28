package objectset

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/metrics"
	"github.com/liyang/weave/pkg/oss/where"
	"github.com/liyang/weave/pkg/types/formula"
)

// LinkTargetTypeResolver is an optional interface that link resolvers can implement
// to provide the "other end" object type API name for a given link traversal.
// For forward traversal "the other end" is the link's declared target; for
// reverse traversal it is the declared source.
type LinkTargetTypeResolver interface {
	ResolveTargetObjectType(ctx context.Context, sourceObjectType, linkTypeAPIName string) (string, error)
}

// DirectionalLinkTargetTypeResolver is the direction-aware superset of
// LinkTargetTypeResolver. Implementations that wish to support reverse
// searchAround traversal should implement this interface in addition to
// LinkTargetTypeResolver.
type DirectionalLinkTargetTypeResolver interface {
	ResolveTargetObjectTypeDir(ctx context.Context, callerObjectType, linkTypeAPIName string, dir links.Direction) (string, error)
}

// InterfaceResolver is an optional dependency the executor uses to evaluate
// interface-scoped ObjectSet definitions ("interfaceBase"). Implementations
// return the list of ObjectType API names that implement the given interface.
// A nil resolver causes interface-scoped definitions to error at execute time.
type InterfaceResolver interface {
	ResolveInterfaceObjectTypes(ctx context.Context, interfaceAPIName string) ([]string, error)
}

// PolicyQueryProvider returns a row-level policy query to AND-combine with
// the executor's Bleve request for the given ObjectType API name. It exists
// so pkg/oss/objectset can call into pkg/security without a direct import,
// keeping the dependency graph flat. A (nil, nil) return signals "no policy
// attached" and the base query flows through unchanged. US-046 wires a
// thin adapter that resolves apiName → RID and forwards to
// *security.Engine.Evaluate.
type PolicyQueryProvider interface {
	PolicyQuery(ctx context.Context, objectType string) (query.Query, error)
}

// EdgePropertiesProvider is the optional hook the executor consults when a
// searchAround step walks a MANY_TO_MANY link (US-210). Implementations
// return a map keyed by the "other end" primary key (target PK in forward,
// source PK in reverse) whose values are the link_edges.edge_properties
// JSONB decoded into a map. When absent the executor skips enrichment and
// Result.EdgeProperties remains nil.
type EdgePropertiesProvider interface {
	ResolveEdgeProperties(ctx context.Context, sourceObjectType, linkAPIName string, sourcePKs []string, dir links.Direction) (map[string]map[string]interface{}, error)
}

// Executor evaluates ObjectSet definitions.
type Executor struct {
	indexMgr       *index.Manager
	linkResolver   links.LinkResolver
	store          *Store
	interfaceResol InterfaceResolver
	vectorStore    NNVectorStore
	embedProvider  NNEmbeddingProvider
	policyProvider PolicyQueryProvider
	edgeProps      EdgePropertiesProvider
	tierRouter     TierRouter
	hotWindow      time.Duration
	tierNowFn      func() time.Time
}

// NewExecutor creates a new ObjectSet executor.
func NewExecutor(indexMgr *index.Manager, linkResolver links.LinkResolver, store *Store) *Executor {
	return &Executor{
		indexMgr:     indexMgr,
		linkResolver: linkResolver,
		store:        store,
	}
}

// SetInterfaceResolver wires an optional InterfaceResolver so the executor can
// evaluate "interfaceBase" ObjectSet definitions without requiring it at
// construction time. Leaving this unset causes such definitions to error.
func (e *Executor) SetInterfaceResolver(r InterfaceResolver) {
	e.interfaceResol = r
}

// SetPolicyProvider wires the optional row-level policy query provider
// (US-046). When attached the executor AND-combines the per-object-type
// policy query into its executeBase / executeFilter Bleve requests so
// LoadObjectSet and Aggregate paths see the same row filtering that
// ServiceImpl applies to Load / Search. Passing nil detaches the hook.
func (e *Executor) SetPolicyProvider(p PolicyQueryProvider) {
	e.policyProvider = p
}

// SetEdgePropertiesProvider wires the optional edge-property enrichment
// provider (US-210). When attached, executeSearchAround populates
// Result.EdgeProperties with a map keyed by the "other end" primary key
// whose value is the per-edge properties JSON decoded into a map. When
// unset the searchAround path behaves exactly as before — no enrichment,
// Result.EdgeProperties stays nil.
func (e *Executor) SetEdgePropertiesProvider(p EdgePropertiesProvider) {
	e.edgeProps = p
}

// resolvePolicyQuery looks up the policy query for objectType via the
// installed provider. It returns (nil, nil) when no provider is installed
// or the provider declines to emit a clause (no policy attached); callers
// MUST treat a nil return as "no extra filter" and use their base query
// unchanged. A degenerate match-all clause is also collapsed to nil so the
// caller does not wrap in a redundant ConjunctionQuery.
func (e *Executor) resolvePolicyQuery(ctx context.Context, objectType string) (query.Query, error) {
	if e.policyProvider == nil {
		return nil, nil
	}
	q, err := e.policyProvider.PolicyQuery(ctx, objectType)
	if err != nil {
		return nil, err
	}
	if q == nil {
		return nil, nil
	}
	if _, ok := q.(*query.MatchAllQuery); ok {
		return nil, nil
	}
	return q, nil
}

// mergePolicyQuery AND-combines a user query with the compiled policy
// query; returns userQ unchanged when policyQ is nil. Mirrors the helper
// in pkg/oss/service_impl.go so both entry points ship the same idiom.
func mergePolicyQuery(userQ, policyQ query.Query) query.Query {
	if policyQ == nil {
		return userQ
	}
	return bleve.NewConjunctionQuery(userQ, policyQ)
}

// BaseExecutionCap is the maximum number of primary keys executeBase will
// return from a single Bleve query. When a result hits this cap the Result's
// Truncated flag is set so callers can warn the user that the answer is
// approximate.
const BaseExecutionCap = 10000

// SearchAroundIntermediateCap bounds the deduped working set between hops in
// a multi-hop searchAround path traversal (US-366). Crossing it aborts the
// walk with ErrQueryTooLarge so callers receive a typed 422 instead of
// silently OOM-ing the executor. The threshold is intentionally generous —
// a single intermediate hop carrying ~1M primary keys is already a sign the
// caller should pre-filter the inner ObjectSet.
const SearchAroundIntermediateCap = 1_000_000

// ErrQueryTooLarge is returned by a multi-hop searchAround whose intermediate
// working set exceeds SearchAroundIntermediateCap. The handler layer (see
// pkg/oss/objectset/handler.go) maps it to APIError code
// WEAVE_QUERY_TOO_LARGE / HTTP 422 so SDK clients can surface a stable code
// instead of parsing the wrapped error message.
var ErrQueryTooLarge = errors.New("WEAVE_QUERY_TOO_LARGE: searchAround intermediate result exceeds cap")

// Result holds the execution result.
type Result struct {
	ObjectType  string   // the object type API name
	PrimaryKeys []string // the matching primary keys
	// Truncated is set to true when the underlying query hit the executor's
	// hard cap (BaseExecutionCap) and the returned PrimaryKeys are only an
	// approximate prefix of the true result. Downstream APIs should surface
	// this as an APPROXIMATE marker so the user knows the answer is partial.
	Truncated bool
	// DerivedValues carries the per-base-object metrics produced by a
	// withProperties execution, keyed by primary key and then by derived
	// property name. Handlers merge these into WireObject.Properties so
	// derived values appear as regular top-level properties in V2 responses.
	// Nil for any execution that does not declare withProperties.
	DerivedValues map[string]map[string]interface{}
	// PerTypePKs is populated by interfaceBase execution with one entry
	// per implementing ObjectType, each sorted ASC by primary key. It is
	// the input to composite-cursor heap-merge pagination at the handler
	// layer (US-007). Nil for non-interfaceBase results.
	PerTypePKs map[string][]string
	// Origins is populated alongside PerTypePKs and is parallel to
	// PrimaryKeys: Origins[i] is the ObjectType API name that contributed
	// PrimaryKeys[i]. Kept so downstream consumers can recover per-row
	// type info without re-walking PerTypePKs.
	Origins []string
	// EdgeProperties carries the per-edge properties produced by a
	// searchAround step over a MANY_TO_MANY link (US-210). Keyed by the
	// "other end" primary key (target PK in forward direction, source PK in
	// reverse) whose value is the decoded JSON map from
	// link_edges.edge_properties. Nil for non-searchAround results, for M2M
	// searchAround when no EdgePropertiesProvider is wired, or when no
	// resolved edge carries any properties.
	EdgeProperties map[string]map[string]interface{}
}

// Execute evaluates an ObjectSet definition and returns matching primary keys.
//
// PRD-V2 §4.6 Gap-O1 wiring: the call is wrapped with a deferred Observe
// onto weave_objectset_execute_duration_seconds, labeled by definition
// kind (base / filter / union / …) and outcome (ok | error). The metric
// is captured at THIS layer rather than inside the per-kind helpers so
// composite ObjectSets (union/intersect/subtract) record one observation
// per public Execute call rather than one per recursive child — the
// Grafana panel asks "how long did this operator take end-to-end?",
// not "how long did each subtree take?". Use the recursive private
// execute() if you need per-subtree timings in the future.
func (e *Executor) Execute(ctx context.Context, def *Definition) (*Result, error) {
	if err := def.Validate(); err != nil {
		// Validation failure happens before we've classified the
		// definition; record it under definition_type="invalid" so the
		// dashboard can spot SDK clients sending malformed bodies
		// without conflating them with backend-level errors.
		metrics.ObserveObjectSetExecute("invalid", "error", 0)
		// Round 37: wrap with the user-side sentinel so handlers route
		// to 400 InvalidObjectSet rather than 500 — Definition.Validate
		// only reports shape problems the caller can fix.
		return nil, fmt.Errorf("%w: %w", ErrInvalidObjectSetDefinition, err)
	}
	defType := def.Type
	start := time.Now()
	res, err := e.execute(ctx, def)
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	metrics.ObserveObjectSetExecute(defType, outcome, time.Since(start).Seconds())
	return res, err
}

func (e *Executor) execute(ctx context.Context, def *Definition) (*Result, error) {
	switch def.Type {
	case "base":
		return e.executeBase(ctx, def)
	case "filter":
		return e.executeFilter(ctx, def)
	case "union":
		return e.executeUnion(ctx, def)
	case "intersect":
		return e.executeIntersect(ctx, def)
	case "subtract":
		return e.executeSubtract(ctx, def)
	case "searchAround":
		return e.executeSearchAround(ctx, def)
	case "reference":
		return e.executeReference(ctx, def)
	case "nearestNeighbors":
		return e.executeNearestNeighbors(ctx, def)
	case "withProperties":
		return e.executeWithProperties(ctx, def)
	case "static":
		return e.executeStatic(ctx, def)
	case "asType":
		return e.executeAsType(ctx, def)
	case "asBaseObjectTypes":
		return e.executeAsBaseObjectTypes(ctx, def)
	case "interfaceBase":
		return e.executeInterfaceBase(ctx, def)
	case "interfaceLinkSearchAround":
		return e.executeInterfaceLinkSearchAround(ctx, def)
	case "sample":
		return e.executeSample(ctx, def)
	case "methodInput":
		// Round 37: caller-side error — methodInput is a user-supplied
		// type the executor explicitly rejects at runtime.
		return nil, fmt.Errorf("%w: methodInput objectSet not yet supported: bind at function invocation time",
			ErrInvalidObjectSetDefinition)
	default:
		// Round 37: caller supplied an unknown type. User-side bug.
		return nil, fmt.Errorf("%w: unknown objectSet type: %q",
			ErrInvalidObjectSetDefinition, def.Type)
	}
}

// executeStatic returns the caller-supplied PrimaryKeys verbatim. No index
// query is performed; the caller has already asserted which objects belong
// to the set.
func (e *Executor) executeStatic(ctx context.Context, def *Definition) (*Result, error) {
	pks := make([]string, len(def.PrimaryKeys))
	copy(pks, def.PrimaryKeys)
	return &Result{
		ObjectType:  def.ObjectType,
		PrimaryKeys: pks,
	}, nil
}

// executeAsType evaluates the inner ObjectSet and relabels the result under
// the requested ObjectType. Intended for interface implementations — the
// caller promises the PKs also exist as instances of the target ObjectType.
func (e *Executor) executeAsType(ctx context.Context, def *Definition) (*Result, error) {
	inner, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, fmt.Errorf("execute asType inner: %w", err)
	}
	return &Result{
		ObjectType:  def.ObjectType,
		PrimaryKeys: inner.PrimaryKeys,
		Truncated:   inner.Truncated,
	}, nil
}

// executeAsBaseObjectTypes passes the inner result through unchanged. In
// Weave's single-type execution model every Result is already attributed to
// a concrete base ObjectType, so "downgrade to base" is a no-op at this layer.
func (e *Executor) executeAsBaseObjectTypes(ctx context.Context, def *Definition) (*Result, error) {
	return e.execute(ctx, def.ObjectSet)
}

// executeInterfaceBase resolves the interface to its implementing ObjectTypes
// via the InterfaceResolver, queries each type's Bleve index concurrently,
// and heap-merges the per-type PK lists into a globally-sorted flat stream.
// Per-type buckets (sorted ASC) are retained on Result.PerTypePKs so the
// handler layer can drive composite-cursor pagination (US-007).
func (e *Executor) executeInterfaceBase(ctx context.Context, def *Definition) (*Result, error) {
	if e.interfaceResol == nil {
		return nil, fmt.Errorf("interfaceBase: interface resolver not configured")
	}
	types, err := e.interfaceResol.ResolveInterfaceObjectTypes(ctx, def.InterfaceType)
	if err != nil {
		return nil, fmt.Errorf("interfaceBase resolve %q: %w", def.InterfaceType, err)
	}

	// Load every implementing ObjectType concurrently. Each goroutine owns
	// a slice result that is deposited under a mutex — no shared growth.
	var (
		mu        sync.Mutex
		perType   = make(map[string][]string, len(types))
		loadErr   error
		truncated bool
		wg        sync.WaitGroup
	)
	for _, objType := range types {
		objType := objType
		wg.Add(1)
		go func() {
			defer wg.Done()
			sub, subErr := e.executeBase(ctx, &Definition{Type: "base", ObjectType: objType})
			mu.Lock()
			defer mu.Unlock()
			if subErr != nil {
				if loadErr == nil {
					loadErr = fmt.Errorf("interfaceBase %s: %w", objType, subErr)
				}
				return
			}
			if sub.Truncated {
				truncated = true
			}
			sorted := make([]string, len(sub.PrimaryKeys))
			copy(sorted, sub.PrimaryKeys)
			sort.Strings(sorted)
			perType[objType] = sorted
		}()
	}
	wg.Wait()
	if loadErr != nil {
		return nil, loadErr
	}

	// Heap-merge the sorted per-type buckets into a globally sorted stream.
	// Stable tiebreaker: (pk, objectType) so identical PKs across types have
	// a deterministic order even though PK collisions are not expected.
	sortedTypes := make([]string, 0, len(perType))
	for t := range perType {
		sortedTypes = append(sortedTypes, t)
	}
	sort.Strings(sortedTypes)

	heads := make(map[string]int, len(sortedTypes))
	active := make([]string, 0, len(sortedTypes))
	for _, t := range sortedTypes {
		if len(perType[t]) > 0 {
			active = append(active, t)
		}
	}

	totalSize := 0
	for _, t := range sortedTypes {
		totalSize += len(perType[t])
	}
	flatPKs := make([]string, 0, totalSize)
	origins := make([]string, 0, totalSize)
	for len(active) > 0 {
		minIdx := 0
		for i := 1; i < len(active); i++ {
			tMin := active[minIdx]
			tCur := active[i]
			pkMin := perType[tMin][heads[tMin]]
			pkCur := perType[tCur][heads[tCur]]
			if pkCur < pkMin || (pkCur == pkMin && tCur < tMin) {
				minIdx = i
			}
		}
		t := active[minIdx]
		flatPKs = append(flatPKs, perType[t][heads[t]])
		origins = append(origins, t)
		heads[t]++
		if heads[t] >= len(perType[t]) {
			active = append(active[:minIdx], active[minIdx+1:]...)
		}
	}

	return &Result{
		// Tag the result with the interface API name so downstream callers
		// know this is a polymorphic set. Per-row concrete type is carried
		// in Origins; per-type buckets live in PerTypePKs for cursor use.
		ObjectType:  def.InterfaceType,
		PrimaryKeys: flatPKs,
		Origins:     origins,
		PerTypePKs:  perType,
		Truncated:   truncated,
	}, nil
}

// executeInterfaceLinkSearchAround walks an interface link from the inner
// ObjectSet. Foundry models interface links as named traversals on the
// implementing ObjectTypes, so we delegate to the regular link resolver using
// the interfaceLink API name as the link identifier.
func (e *Executor) executeInterfaceLinkSearchAround(ctx context.Context, def *Definition) (*Result, error) {
	sourceResult, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, fmt.Errorf("execute interfaceLinkSearchAround source: %w", err)
	}
	linkedPKs, err := e.linkResolver.ResolveLinkedObjectsByAPIName(ctx, sourceResult.ObjectType, def.InterfaceLink, sourceResult.PrimaryKeys)
	if err != nil {
		return nil, fmt.Errorf("resolve interfaceLinkSearchAround: %w", err)
	}

	targetType := ""
	if resolver, ok := e.linkResolver.(LinkTargetTypeResolver); ok {
		targetType, _ = resolver.ResolveTargetObjectType(ctx, sourceResult.ObjectType, def.InterfaceLink)
	}

	return &Result{
		ObjectType:  targetType,
		PrimaryKeys: linkedPKs,
		Truncated:   sourceResult.Truncated,
	}, nil
}

// executeBase queries the Bleve index for ALL objects of the given type, up
// to BaseExecutionCap. If the returned hit count equals the cap the result is
// flagged as Truncated so the caller can surface an APPROXIMATE warning.
//
// US-407: when a TierRouter is wired the executor also fans out to the cold
// (Parquet) tier for rows older than `now - hotWindow` and merges the two
// streams hot-wins. Hot ordering is preserved; cold-only PKs append in the
// order the cold tier returned them.
//
// US-485: when the caller declares a Definition.TimeRange the executor
// short-circuits the irrelevant tier:
//   - Hot-only window (From ≥ now-hotWindow): skip the cold lookup entirely.
//   - Cold-only window (To ≤ now-hotWindow): skip the Bleve search and
//     pass the request's upper bound to the cold tier as the cutoff so
//     materialize rows beyond the window are clipped before merge.
//   - Cross-window (straddles now-hotWindow): both tiers, classic union.
func (e *Executor) executeBase(ctx context.Context, def *Definition) (*Result, error) {
	policyQ, err := e.resolvePolicyQuery(ctx, def.ObjectType)
	if err != nil {
		return nil, fmt.Errorf("resolve policy for base objectSet %q: %w", def.ObjectType, err)
	}

	scopedKey := scopedIndexKey(ctx, e.indexMgr, def.ObjectType)

	// US-408: when an index rebuild is in flight the Bleve index is
	// either gone (between Drop and EnsureIndex) or partially populated
	// (mid-batch). Skip the hot-tier read entirely and serve the request
	// from the cold (Parquet) tier with a now-cutoff so callers see the
	// full materialised set rather than a half-empty index. When no cold
	// tier is wired we degrade to an empty result rather than surfacing
	// an "index not found" 5xx — operators who triggered the rebuild
	// already accepted the read-availability trade-off.
	if e.indexMgr.IsRebuilding(scopedKey) {
		return e.executeBaseDuringRebuild(ctx, def)
	}

	routing := classifyTierRouting(def.TimeRange, e.effectiveNow(), e.effectiveHotWindow())

	var (
		pks       []string
		truncated bool
	)
	if routing.queryHot {
		searchReq := bleve.NewSearchRequest(mergePolicyQuery(bleve.NewMatchAllQuery(), policyQ))
		searchReq.Size = BaseExecutionCap
		searchReq.Fields = []string{"*"}

		result, err := e.indexMgr.Search(scopedKey, searchReq)
		if err != nil {
			return nil, fmt.Errorf("search base objectSet %q: %w", def.ObjectType, err)
		}
		pks = make([]string, 0, len(result.Hits))
		for _, hit := range result.Hits {
			pks = append(pks, hit.ID)
		}
		truncated = len(pks) >= BaseExecutionCap
	}

	if e.tierRouter != nil && routing.queryCold {
		ontology := OntologyScopeFromContextOrEmpty(ctx)
		coldPKs, coldErr := e.tierRouter.ColdPrimaryKeys(ctx, ontology, def.ObjectType, routing.coldCutoff)
		if coldErr != nil {
			return nil, fmt.Errorf("cold tier base objectSet %q: %w", def.ObjectType, coldErr)
		}
		if len(coldPKs) > 0 {
			pks = mergeHotColdPKs(pks, coldPKs)
		}
	}

	return &Result{
		ObjectType:  def.ObjectType,
		PrimaryKeys: pks,
		Truncated:   truncated,
	}, nil
}

// executeBaseDuringRebuild routes a base ObjectSet read entirely through
// the cold tier (US-408). The cutoff is e.effectiveNow() so the cold view
// returns every materialised PK, including the rows that would normally
// live in the hot tier — without this widening the executor would return
// only rows older than `now - hotWindow` while the index is being
// rebuilt, hiding live data behind the rolling window.
//
// When no cold tier is wired this returns an empty result. That is the
// correct degraded-mode behavior: the rebuild was operator-initiated
// and the operator accepted that base reads degrade until the rebuild
// completes.
func (e *Executor) executeBaseDuringRebuild(ctx context.Context, def *Definition) (*Result, error) {
	if e.tierRouter == nil {
		return &Result{ObjectType: def.ObjectType, PrimaryKeys: nil}, nil
	}
	ontology := OntologyScopeFromContextOrEmpty(ctx)
	coldPKs, err := e.tierRouter.ColdPrimaryKeys(ctx, ontology, def.ObjectType, e.effectiveNow())
	if err != nil {
		return nil, fmt.Errorf("cold tier base objectSet %q during rebuild: %w", def.ObjectType, err)
	}
	return &Result{
		ObjectType:  def.ObjectType,
		PrimaryKeys: coldPKs,
	}, nil
}

// executeFilter executes the base objectSet, then applies the where clause filter.
func (e *Executor) executeFilter(ctx context.Context, def *Definition) (*Result, error) {
	baseResult, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, fmt.Errorf("execute filter base: %w", err)
	}

	// Parse the where clause. Bad JSON is user-side; wrap with the
	// round-37 sentinel so the handler routes to 400 InvalidObjectSet
	// rather than 500.
	var clause where.WhereClause
	if err := json.Unmarshal(def.Where, &clause); err != nil {
		return nil, fmt.Errorf("%w: parse where clause: %w", ErrInvalidObjectSetDefinition, err)
	}

	// Convert to Bleve query. The converter already wraps its errors
	// with where.ErrInvalidWhereClause (round 36); just chain via %w
	// so the handler's errors.Is at the handler boundary still sees
	// the sentinel.
	bleveQuery, err := where.ConvertToBleveQuery(&clause)
	if err != nil {
		return nil, fmt.Errorf("convert where clause: %w", err)
	}

	// Build a query that intersects the base PKs with the where filter.
	// Use DocIDQuery to limit to the base result PKs, combined with the where filter.
	// US-046: AND-combine the row-level policy query so deny-listed rows
	// can't slip back in through the doc-id branch. The base PKs are
	// already policy-filtered via executeBase, but re-applying here keeps
	// the where-side query self-sufficient and makes the invariant local.
	policyQ, err := e.resolvePolicyQuery(ctx, baseResult.ObjectType)
	if err != nil {
		return nil, fmt.Errorf("resolve policy for filter objectSet %q: %w", baseResult.ObjectType, err)
	}
	docIDQ := bleve.NewDocIDQuery(baseResult.PrimaryKeys)
	conjQ := bleve.NewConjunctionQuery(docIDQ, mergePolicyQuery(bleveQuery, policyQ))

	searchReq := bleve.NewSearchRequest(conjQ)
	searchReq.Size = BaseExecutionCap
	result, err := e.indexMgr.Search(scopedIndexKey(ctx, e.indexMgr, baseResult.ObjectType), searchReq)
	if err != nil {
		return nil, fmt.Errorf("search filter objectSet: %w", err)
	}

	pks := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		pks = append(pks, hit.ID)
	}

	return &Result{
		ObjectType:  baseResult.ObjectType,
		PrimaryKeys: pks,
		// Propagate truncation from the inner set, and also flag the filter
		// itself if it hit the cap.
		Truncated: baseResult.Truncated || len(pks) >= BaseExecutionCap,
	}, nil
}

// executeUnion executes all sub-ObjectSets and unions the results.
func (e *Executor) executeUnion(ctx context.Context, def *Definition) (*Result, error) {
	seen := make(map[string]bool)
	var allPKs []string
	var objectType string
	truncated := false

	for _, sub := range def.ObjectSets {
		result, err := e.execute(ctx, sub)
		if err != nil {
			return nil, fmt.Errorf("execute union sub: %w", err)
		}
		if objectType == "" {
			objectType = result.ObjectType
		}
		if result.Truncated {
			truncated = true
		}
		for _, pk := range result.PrimaryKeys {
			if !seen[pk] {
				allPKs = append(allPKs, pk)
				seen[pk] = true
			}
		}
	}

	return &Result{ObjectType: objectType, PrimaryKeys: allPKs, Truncated: truncated}, nil
}

// executeIntersect executes all sub-ObjectSets and intersects the results.
func (e *Executor) executeIntersect(ctx context.Context, def *Definition) (*Result, error) {
	var objectType string
	truncated := false

	// Count occurrences of each PK across all sub-ObjectSets
	counts := make(map[string]int)
	for i, sub := range def.ObjectSets {
		result, err := e.execute(ctx, sub)
		if err != nil {
			return nil, fmt.Errorf("execute intersect sub: %w", err)
		}
		if i == 0 {
			objectType = result.ObjectType
		}
		if result.Truncated {
			truncated = true
		}
		for _, pk := range result.PrimaryKeys {
			counts[pk]++
		}
	}

	// Only keep PKs that appear in ALL sub-ObjectSets
	total := len(def.ObjectSets)
	var pks []string
	for pk, count := range counts {
		if count == total {
			pks = append(pks, pk)
		}
	}

	return &Result{ObjectType: objectType, PrimaryKeys: pks, Truncated: truncated}, nil
}

// executeSubtract executes all sub-ObjectSets and subtracts subsequent sets from the first.
func (e *Executor) executeSubtract(ctx context.Context, def *Definition) (*Result, error) {
	// Execute the first sub-ObjectSet as the base
	firstResult, err := e.execute(ctx, def.ObjectSets[0])
	if err != nil {
		return nil, fmt.Errorf("execute subtract base: %w", err)
	}
	truncated := firstResult.Truncated

	// Collect all PKs to exclude from subsequent sub-ObjectSets
	exclude := make(map[string]bool)
	for _, sub := range def.ObjectSets[1:] {
		result, err := e.execute(ctx, sub)
		if err != nil {
			return nil, fmt.Errorf("execute subtract sub: %w", err)
		}
		if result.Truncated {
			truncated = true
		}
		for _, pk := range result.PrimaryKeys {
			exclude[pk] = true
		}
	}

	// Keep only PKs from the first set that are not excluded
	var pks []string
	for _, pk := range firstResult.PrimaryKeys {
		if !exclude[pk] {
			pks = append(pks, pk)
		}
	}

	return &Result{ObjectType: firstResult.ObjectType, PrimaryKeys: pks, Truncated: truncated}, nil
}

// executeSearchAround uses the link resolver to find objects linked to the source set.
// If def.Direction == "reverse" the link is walked target -> source, meaning
// the input ObjectSet contains objects of the link's declared *target* and the
// output contains objects of the declared *source*.
//
// When def.Path is set the executor walks each step in order, threading the
// deduped PKs and resolved ObjectType from hop N into hop N+1. See
// executeSearchAroundPath for the multi-hop implementation (US-226).
func (e *Executor) executeSearchAround(ctx context.Context, def *Definition) (*Result, error) {
	if len(def.Path) > 0 {
		return e.executeSearchAroundPath(ctx, def)
	}
	sourceResult, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, fmt.Errorf("execute searchAround source: %w", err)
	}

	dir, err := links.ParseDirection(def.Direction)
	if err != nil {
		return nil, fmt.Errorf("searchAround direction: %w", err)
	}

	// Forward: use legacy API name path (unchanged behavior).
	// Reverse: the inner set's ObjectType is the link's *target*, so we cannot
	// use ResolveLinkedObjectsByAPIName (which scans outgoing links from the
	// source). Instead, find the link via the OMS repo-backed resolver and
	// call the direction-aware ResolveLinked.
	var linkedPKs []string
	if dir == links.DirectionForward {
		linkedPKs, err = e.linkResolver.ResolveLinkedObjectsByAPIName(ctx, sourceResult.ObjectType, def.Link, sourceResult.PrimaryKeys)
	} else {
		if finder, ok := e.linkResolver.(reverseLinkFinder); ok {
			linkedPKs, err = finder.ResolveLinkedReverseByAPIName(ctx, sourceResult.ObjectType, def.Link, sourceResult.PrimaryKeys)
		} else {
			return nil, fmt.Errorf("link resolver does not support reverse searchAround")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("resolve searchAround links: %w", err)
	}

	// Resolve the "other end" object type if the link resolver supports it.
	// Prefer the direction-aware interface, fall back to the legacy forward-only one.
	targetType := ""
	if resolver, ok := e.linkResolver.(DirectionalLinkTargetTypeResolver); ok {
		targetType, _ = resolver.ResolveTargetObjectTypeDir(ctx, sourceResult.ObjectType, def.Link, dir)
	} else if resolver, ok := e.linkResolver.(LinkTargetTypeResolver); ok && dir == links.DirectionForward {
		targetType, _ = resolver.ResolveTargetObjectType(ctx, sourceResult.ObjectType, def.Link)
	}

	// US-210: optional edge-property enrichment. Only runs when a provider
	// is wired AND the inner set produced at least one PK. Failures are
	// non-fatal — traversal stays correct even if enrichment is unavailable.
	var edgeProps map[string]map[string]interface{}
	if e.edgeProps != nil && len(sourceResult.PrimaryKeys) > 0 && len(linkedPKs) > 0 {
		ep, err := e.edgeProps.ResolveEdgeProperties(ctx, sourceResult.ObjectType, def.Link, sourceResult.PrimaryKeys, dir)
		if err == nil && len(ep) > 0 {
			edgeProps = ep
		}
	}

	return &Result{
		ObjectType:  targetType,
		PrimaryKeys: linkedPKs,
		// Inherit truncation from the source set: if the inner set was already
		// approximate, the searchAround output is also approximate.
		Truncated:      sourceResult.Truncated,
		EdgeProperties: edgeProps,
	}, nil
}

// reverseLinkFinder is an optional interface that resolvers implement to
// expose reverse-traversal-by-API-name for the ObjectSet executor. It is
// separate from LinkResolver so reverse support remains opt-in.
type reverseLinkFinder interface {
	ResolveLinkedReverseByAPIName(ctx context.Context, callerObjectType, linkTypeAPIName string, callerPKs []string) ([]string, error)
}

// executeSearchAroundPath walks def.Path as an ordered chain of link hops.
// Each hop threads the deduped PK set from the previous hop as input and
// uses the resolved (or declared) ObjectType of the previous hop as its
// source type. When a step carries ExpectedObjectType the executor asserts
// the resolver's target-type lookup matches it — a mismatch aborts the walk
// with a descriptive error so cross-hop paths fail loudly instead of
// silently walking the wrong index.
//
// Cycle detection (US-366): the executor maintains a cross-hop visited set
// keyed by (objectType, primaryKey). After each hop's resolver call, PKs
// already seen at the same ObjectType are pruned so traversals like
// A→B→A→B do not re-walk previously visited nodes (and therefore terminate
// in finite work even when path length and the link graph could in
// principle cycle indefinitely). The visited set is seeded with the source
// PKs so paths that step back to the origin shrink to zero on hop 1.
//
// Intermediate-size cap (US-366): when the deduped+pruned working set
// crosses SearchAroundIntermediateCap the walk aborts with ErrQueryTooLarge
// — the handler maps this to WEAVE_QUERY_TOO_LARGE / HTTP 422 so callers
// see a stable error code instead of an OOM.
//
// Edge-property enrichment is intentionally not surfaced for path-based
// searchAround; the single-hop shape remains the channel for per-edge props.
func (e *Executor) executeSearchAroundPath(ctx context.Context, def *Definition) (*Result, error) {
	sourceResult, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, fmt.Errorf("execute searchAround source: %w", err)
	}

	currentType := sourceResult.ObjectType
	currentPKs := sourceResult.PrimaryKeys
	truncated := sourceResult.Truncated

	// Cross-hop visited set keyed by "objectType\x00primaryKey". Seeded with
	// the source set so the very first hop already prunes any link that
	// loops back to the origin.
	visited := make(map[string]bool, len(currentPKs))
	for _, pk := range currentPKs {
		visited[visitedKey(currentType, pk)] = true
	}
	if len(currentPKs) > SearchAroundIntermediateCap {
		return nil, fmt.Errorf("%w: source set has %d primary keys (cap %d)",
			ErrQueryTooLarge, len(currentPKs), SearchAroundIntermediateCap)
	}

	for i, step := range def.Path {
		dir, err := links.ParseDirection(step.Direction)
		if err != nil {
			return nil, fmt.Errorf("searchAround path[%d]: %w", i, err)
		}

		resolvedTargetType := ""
		if resolver, ok := e.linkResolver.(DirectionalLinkTargetTypeResolver); ok {
			resolvedTargetType, _ = resolver.ResolveTargetObjectTypeDir(ctx, currentType, step.Link, dir)
		} else if resolver, ok := e.linkResolver.(LinkTargetTypeResolver); ok && dir == links.DirectionForward {
			resolvedTargetType, _ = resolver.ResolveTargetObjectType(ctx, currentType, step.Link)
		}

		if step.ExpectedObjectType != "" && resolvedTargetType != "" && resolvedTargetType != step.ExpectedObjectType {
			return nil, fmt.Errorf("searchAround path[%d] (link %q): expectedObjectType %q does not match resolved target %q",
				i, step.Link, step.ExpectedObjectType, resolvedTargetType)
		}

		// Resolve the next-hop ObjectType up front so empty intermediate
		// sets still pick the right currentType when we short-circuit.
		nextType := ""
		switch {
		case resolvedTargetType != "":
			nextType = resolvedTargetType
		case step.ExpectedObjectType != "":
			nextType = step.ExpectedObjectType
		}

		if len(currentPKs) == 0 {
			// Empty sets stay empty through subsequent hops; pick the best
			// known target type so downstream callers still see the right
			// result.ObjectType.
			currentPKs = nil
			currentType = nextType
			continue
		}

		var nextPKs []string
		if dir == links.DirectionForward {
			nextPKs, err = e.linkResolver.ResolveLinkedObjectsByAPIName(ctx, currentType, step.Link, currentPKs)
		} else {
			finder, ok := e.linkResolver.(reverseLinkFinder)
			if !ok {
				return nil, fmt.Errorf("searchAround path[%d]: link resolver does not support reverse traversal", i)
			}
			nextPKs, err = finder.ResolveLinkedReverseByAPIName(ctx, currentType, step.Link, currentPKs)
		}
		if err != nil {
			return nil, fmt.Errorf("searchAround path[%d] resolve %q: %w", i, step.Link, err)
		}
		nextPKs = dedupeStrings(nextPKs)

		// Cycle prune: drop any PK we've already visited at this
		// ObjectType. The remaining set is what the *next* hop reads from
		// and is also what the final result.PrimaryKeys returns when this
		// is the last step.
		if len(nextPKs) > 0 {
			pruned := nextPKs[:0]
			for _, pk := range nextPKs {
				key := visitedKey(nextType, pk)
				if visited[key] {
					continue
				}
				visited[key] = true
				pruned = append(pruned, pk)
			}
			nextPKs = pruned
		}

		if len(nextPKs) > SearchAroundIntermediateCap {
			return nil, fmt.Errorf("%w: hop %d (link %q) produced %d primary keys (cap %d)",
				ErrQueryTooLarge, i, step.Link, len(nextPKs), SearchAroundIntermediateCap)
		}

		currentType = nextType
		currentPKs = nextPKs
	}

	return &Result{
		ObjectType:  currentType,
		PrimaryKeys: currentPKs,
		Truncated:   truncated,
	}, nil
}

// visitedKey is the canonical map key for the cross-hop searchAround
// cycle-prune set. Using a NUL separator keeps it injection-free since
// neither ObjectType API names nor primary keys contain NUL.
func visitedKey(objectType, pk string) string {
	return objectType + "\x00" + pk
}

// dedupeStrings returns pks with duplicates removed while preserving the
// first-seen order. Returns a nil slice when pks is empty so callers can
// feed it straight into the next hop without a special case.
func dedupeStrings(pks []string) []string {
	if len(pks) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(pks))
	out := make([]string, 0, len(pks))
	for _, pk := range pks {
		if !seen[pk] {
			seen[pk] = true
			out = append(out, pk)
		}
	}
	return out
}

// executeWithProperties executes the inner ObjectSet, then evaluates each
// declared derived property as a single-hop link aggregation per base object.
// When no DerivedProperties are declared the call degenerates to the inner
// ObjectSet so pre-existing callers that only used Properties keep working.
//
// Scope: count / sum / avg / min / max over both forward and reverse
// directions. Reverse-direction numeric metrics walk
// reverseLinkFinder.ResolveLinkedReverseByAPIName for link traversal and
// DirectionalLinkTargetTypeResolver.ResolveTargetObjectTypeDir for the
// "other end" ObjectType discovery; resolvers that satisfy only the legacy
// forward-only interfaces surface a clear "reverse not supported" error
// instead of silently returning zeros.
func (e *Executor) executeWithProperties(ctx context.Context, def *Definition) (*Result, error) {
	inner, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, err
	}
	if len(def.DerivedProperties) == 0 {
		return inner, nil
	}

	// Polymorphic path: when the inner ObjectSet is an interfaceBase (or any
	// other operator that produces per-type buckets), compute derived values
	// per concrete ObjectType so the link resolver sees the real source type,
	// not the interface API name. Values are keyed by (type, pk) so PK
	// collisions across types do not clobber each other.
	if inner.PerTypePKs != nil {
		return e.executeWithPropertiesPolymorphic(ctx, def, inner)
	}

	derived := make(map[string]map[string]interface{}, len(inner.PrimaryKeys))
	for _, pk := range inner.PrimaryKeys {
		derived[pk] = make(map[string]interface{}, len(def.DerivedProperties))
	}

	// Lazy-loaded base object fields keyed by primary key. Only populated
	// when at least one derived property needs it (formula metric), so the
	// existing link-based metrics don't pay the extra Bleve search.
	var baseFields map[string]map[string]interface{}
	loadBase := func(ctx context.Context) error {
		if baseFields != nil {
			return nil
		}
		fields, err := e.loadBaseObjectFields(ctx, inner.ObjectType, inner.PrimaryKeys)
		if err != nil {
			return fmt.Errorf("withProperties: load base fields for %q: %w", inner.ObjectType, err)
		}
		baseFields = fields
		return nil
	}

	for _, dp := range def.DerivedProperties {
		if dp.IsFormula() {
			if err := loadBase(ctx); err != nil {
				return nil, err
			}
			if err := e.evaluateFormulaDerived(ctx, dp, inner.PrimaryKeys, baseFields, derived); err != nil {
				return nil, err
			}
			continue
		}
		dir, err := links.ParseDirection(dp.Direction)
		if err != nil {
			return nil, fmt.Errorf("withProperties %q: %w", dp.Name, err)
		}

		resolveLinks, err := e.linkResolverForDirection(dp, dir)
		if err != nil {
			return nil, err
		}

		switch dp.Metric {
		case "count":
			for _, pk := range inner.PrimaryKeys {
				targets, err := resolveLinks(ctx, inner.ObjectType, dp.Link, []string{pk})
				if err != nil {
					return nil, fmt.Errorf("withProperties %q resolve link %q: %w", dp.Name, dp.Link, err)
				}
				derived[pk][dp.Name] = int64(len(targets))
			}
		case "sum", "avg", "min", "max":
			if err := e.evaluateNumericDerived(ctx, inner, dp, dir, resolveLinks, derived); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("withProperties %q: metric %q not yet supported", dp.Name, dp.Metric)
		}
	}

	// US-005: stabilise pagination. Sort PKs by (firstDerivedValue ASC,
	// primaryKey ASC) so offset-based page slicing never drops or duplicates
	// rows even when many base objects share the same derived value. We copy
	// inner.PrimaryKeys first so inner callers (e.g. other operators that
	// reference the same inner Result) observe the order the inner executor
	// returned.
	orderedPKs := make([]string, len(inner.PrimaryKeys))
	copy(orderedPKs, inner.PrimaryKeys)
	sortKey := def.DerivedProperties[0].Name
	sort.SliceStable(orderedPKs, func(i, j int) bool {
		pi, pj := orderedPKs[i], orderedPKs[j]
		if cmp := compareDerivedValues(derived[pi][sortKey], derived[pj][sortKey]); cmp != 0 {
			return cmp < 0
		}
		return pi < pj
	})

	return &Result{
		ObjectType:    inner.ObjectType,
		PrimaryKeys:   orderedPKs,
		Truncated:     inner.Truncated,
		DerivedValues: derived,
	}, nil
}

// polymorphicDerivedKey encodes (objectType, primaryKey) into a single map
// key for Result.DerivedValues when the inner set is polymorphic. Using a NUL
// byte as the separator keeps the key unique even if a primary key legitimately
// contains "|" or ":". The handler's writePreviewInterfacePage and any other
// per-row consumer must read polymorphic derived values through this helper
// instead of indexing DerivedValues by plain primary key.
func polymorphicDerivedKey(objectType, primaryKey string) string {
	return objectType + "\x00" + primaryKey
}

// executeWithPropertiesPolymorphic computes derived values for a polymorphic
// inner Result (one produced by interfaceBase or any other operator that
// populates PerTypePKs). Values are keyed by polymorphicDerivedKey so a PK
// collision between two implementing ObjectTypes cannot clobber either side.
//
// Scope for Phase 6 US-032: forward direction, count metric only. Numeric
// metrics and reverse direction over an interface base are explicitly out of
// scope and surface a clear error so the unsupported surface is obvious.
func (e *Executor) executeWithPropertiesPolymorphic(ctx context.Context, def *Definition, inner *Result) (*Result, error) {
	derived := make(map[string]map[string]interface{}, len(inner.PrimaryKeys))
	for t, pks := range inner.PerTypePKs {
		for _, pk := range pks {
			derived[polymorphicDerivedKey(t, pk)] = make(map[string]interface{}, len(def.DerivedProperties))
		}
	}

	for _, dp := range def.DerivedProperties {
		dir, err := links.ParseDirection(dp.Direction)
		if err != nil {
			return nil, fmt.Errorf("withProperties %q: %w", dp.Name, err)
		}
		if dir != links.DirectionForward {
			return nil, fmt.Errorf("withProperties %q over interfaceBase: reverse direction not yet supported", dp.Name)
		}
		if dp.Metric != "count" {
			return nil, fmt.Errorf("withProperties %q over interfaceBase: metric %q not yet supported (only count)", dp.Name, dp.Metric)
		}
		for t, pks := range inner.PerTypePKs {
			for _, pk := range pks {
				targets, err := e.linkResolver.ResolveLinkedObjectsByAPIName(ctx, t, dp.Link, []string{pk})
				if err != nil {
					return nil, fmt.Errorf("withProperties %q resolve link %q on %q: %w", dp.Name, dp.Link, t, err)
				}
				derived[polymorphicDerivedKey(t, pk)][dp.Name] = int64(len(targets))
			}
		}
	}

	// Preserve the inner executor's per-type buckets + flat/origins order so
	// the handler's composite-cursor paging keeps walking the same sub-streams
	// it always did. The handler merges derived values onto each row via
	// polymorphicDerivedKey(row.objectType, row.pk).
	return &Result{
		ObjectType:    inner.ObjectType,
		PrimaryKeys:   inner.PrimaryKeys,
		Origins:       inner.Origins,
		PerTypePKs:    inner.PerTypePKs,
		Truncated:     inner.Truncated,
		DerivedValues: derived,
	}, nil
}

// compareDerivedValues orders two derived property values so withProperties
// results have a deterministic, offset-pagination-safe order. Numeric values
// (the common case for count / sum / avg / min / max) are compared
// numerically; nil sorts before any concrete value so pages with empty link
// sets land predictably on the left side; non-numeric fall back to a string
// representation so unexpected types still yield a total ordering instead of
// panicking.
func compareDerivedValues(a, b interface{}) int {
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}
	af, aOk := coerceNumeric(a)
	bf, bOk := coerceNumeric(b)
	if aOk && bOk {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		default:
			return 0
		}
	}
	as := fmt.Sprintf("%v", a)
	bs := fmt.Sprintf("%v", b)
	switch {
	case as < bs:
		return -1
	case as > bs:
		return 1
	default:
		return 0
	}
}

// linkResolverForDirection returns a closure that walks dp.Link in the
// requested direction, hiding the forward / reverse split from callers. Reverse
// traversal requires the underlying resolver to implement reverseLinkFinder;
// otherwise we surface an explicit error so the caller cannot silently fall
// through to a forward walk.
func (e *Executor) linkResolverForDirection(dp DerivedPropertyDef, dir links.Direction) (func(context.Context, string, string, []string) ([]string, error), error) {
	if dir == links.DirectionForward {
		return e.linkResolver.ResolveLinkedObjectsByAPIName, nil
	}
	finder, ok := e.linkResolver.(reverseLinkFinder)
	if !ok {
		return nil, fmt.Errorf("withProperties %q: link resolver does not support reverse direction", dp.Name)
	}
	return finder.ResolveLinkedReverseByAPIName, nil
}

// resolveLinkTargetType returns the "other end" ObjectType API name for a
// link walk in the requested direction. Forward direction prefers the
// direction-aware resolver and falls back to the legacy
// LinkTargetTypeResolver; reverse direction requires
// DirectionalLinkTargetTypeResolver because the legacy interface only
// reports the link's declared target. An empty string return means the
// installed resolver cannot answer the question — callers MUST treat that
// as a hard failure so numeric metrics never silently read the wrong index.
func (e *Executor) resolveLinkTargetType(ctx context.Context, callerObjectType, linkAPIName string, dir links.Direction) (string, error) {
	if resolver, ok := e.linkResolver.(DirectionalLinkTargetTypeResolver); ok {
		return resolver.ResolveTargetObjectTypeDir(ctx, callerObjectType, linkAPIName, dir)
	}
	if dir == links.DirectionForward {
		if resolver, ok := e.linkResolver.(LinkTargetTypeResolver); ok {
			return resolver.ResolveTargetObjectType(ctx, callerObjectType, linkAPIName)
		}
	}
	return "", nil
}

// evaluateNumericDerived computes sum / avg / min / max of dp.Field across
// the linked "other end" objects for every base PK in inner. The "other end"
// is direction-aware — for forward traversal it is the link's declared
// target; for reverse traversal it is the link's declared source — so the
// numeric field is read from the correct Bleve index in both cases.
// resolveLinks is the direction-aware link walk produced by
// linkResolverForDirection; callers MUST pass the one matching dp.Direction
// so reverse-direction sums see the right edges. Non-numeric fields surface
// as DerivedPropertyTypeMismatch.
func (e *Executor) evaluateNumericDerived(ctx context.Context, inner *Result, dp DerivedPropertyDef, dir links.Direction, resolveLinks func(context.Context, string, string, []string) ([]string, error), derived map[string]map[string]interface{}) error {
	targetType, err := e.resolveLinkTargetType(ctx, inner.ObjectType, dp.Link, dir)
	if err != nil {
		return fmt.Errorf("withProperties %q resolve target type for link %q: %w", dp.Name, dp.Link, err)
	}
	if targetType == "" {
		return fmt.Errorf("withProperties %q: link resolver cannot determine target ObjectType for link %q (direction=%s)", dp.Name, dp.Link, dir)
	}
	targetIndexKey := scopedIndexKey(ctx, e.indexMgr, targetType)

	for _, pk := range inner.PrimaryKeys {
		targets, err := resolveLinks(ctx, inner.ObjectType, dp.Link, []string{pk})
		if err != nil {
			return fmt.Errorf("withProperties %q resolve link %q: %w", dp.Name, dp.Link, err)
		}
		if len(targets) == 0 {
			switch dp.Metric {
			case "sum":
				derived[pk][dp.Name] = float64(0)
			default: // avg, min, max
				derived[pk][dp.Name] = nil
			}
			continue
		}

		searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery(targets))
		searchReq.Fields = []string{dp.Field}
		searchReq.Size = len(targets)
		res, err := e.indexMgr.Search(targetIndexKey, searchReq)
		if err != nil {
			return fmt.Errorf("withProperties %q fetch targets for %q: %w", dp.Name, pk, err)
		}

		var (
			sum        float64
			count      int
			haveMinMax bool
			minV       float64
			maxV       float64
		)
		for _, hit := range res.Hits {
			raw, ok := hit.Fields[dp.Field]
			if !ok || raw == nil {
				continue
			}
			num, ok := coerceNumeric(raw)
			if !ok {
				return fmt.Errorf("withProperties %q: DerivedPropertyTypeMismatch: field %q on %q is not numeric (got %T)", dp.Name, dp.Field, targetType, raw)
			}
			sum += num
			count++
			if !haveMinMax {
				minV = num
				maxV = num
				haveMinMax = true
			} else {
				if num < minV {
					minV = num
				}
				if num > maxV {
					maxV = num
				}
			}
		}

		if count == 0 {
			switch dp.Metric {
			case "sum":
				derived[pk][dp.Name] = float64(0)
			default:
				derived[pk][dp.Name] = nil
			}
			continue
		}

		switch dp.Metric {
		case "sum":
			derived[pk][dp.Name] = sum
		case "avg":
			derived[pk][dp.Name] = sum / float64(count)
		case "min":
			derived[pk][dp.Name] = minV
		case "max":
			derived[pk][dp.Name] = maxV
		}
	}
	return nil
}

// loadBaseObjectFields batch-fetches the stored field map for each PK in a
// single Bleve DocIDQuery. Missing PKs produce no map entry; formula
// evaluation handles that by passing an empty object through to the JS VM.
// Returns a non-nil map even when pks is empty so callers can safely index.
func (e *Executor) loadBaseObjectFields(ctx context.Context, objectType string, pks []string) (map[string]map[string]interface{}, error) {
	out := make(map[string]map[string]interface{}, len(pks))
	if len(pks) == 0 {
		return out, nil
	}
	searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery(pks))
	searchReq.Fields = []string{"*"}
	searchReq.Size = len(pks)
	indexKey := scopedIndexKey(ctx, e.indexMgr, objectType)
	res, err := e.indexMgr.Search(indexKey, searchReq)
	if err != nil {
		return nil, err
	}
	for _, hit := range res.Hits {
		out[hit.ID] = hit.Fields
	}
	return out, nil
}

// evaluateFormulaDerived compiles dp.Formula once and evaluates it per base
// object, binding the object's stored fields to `this` and `self`. Compile
// errors surface with a "compile" substring so callers can distinguish them
// from runtime errors, which are scoped to the offending PK.
func (e *Executor) evaluateFormulaDerived(ctx context.Context, dp DerivedPropertyDef, pks []string, baseFields map[string]map[string]interface{}, derived map[string]map[string]interface{}) error {
	evaluator, err := formula.New(dp.Formula)
	if err != nil {
		return fmt.Errorf("withProperties %q: %w", dp.Name, err)
	}
	for _, pk := range pks {
		fields := baseFields[pk]
		if fields == nil {
			fields = map[string]interface{}{}
		}
		v, err := evaluator.Evaluate(ctx, fields)
		if err != nil {
			return fmt.Errorf("withProperties %q: evaluate %q: %w", dp.Name, pk, err)
		}
		derived[pk][dp.Name] = v
	}
	return nil
}

// coerceNumeric converts a Bleve field value to float64. Bleve stores numeric
// fields as float64 when round-tripped through Search with Fields=[...], but
// callers may also hand us ints or json.Numbers depending on the indexing
// path. Returns ok=false for anything non-numeric so the executor can emit
// DerivedPropertyTypeMismatch.
func coerceNumeric(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}

// executeSample evaluates the inner ObjectSet and returns at most def.Size
// primary keys chosen uniformly at random via Algorithm R (reservoir
// sampling). When Seed is set the PRNG is seeded deterministically so two
// runs of the same definition produce the same output; when Seed is nil a
// wall-clock seed is used. Size >= len(inner) returns the inner slice in
// place (shuffling is neither required nor performed in that case).
func (e *Executor) executeSample(ctx context.Context, def *Definition) (*Result, error) {
	inner, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, fmt.Errorf("execute sample inner: %w", err)
	}

	size := *def.Size
	// #nosec G404 — math/rand is intentional: reservoir sampling wants a
	// deterministic, reproducible PRNG keyed on the caller-supplied Seed.
	var src rand.Source
	if def.Seed != nil {
		src = rand.NewSource(*def.Seed)
	} else {
		src = rand.NewSource(time.Now().UnixNano())
	}
	rng := rand.New(src)

	candidates := inner.PrimaryKeys
	if def.Seed != nil {
		candidates = append([]string(nil), inner.PrimaryKeys...)
		sort.Strings(candidates)
	}
	sampled := reservoirSample(candidates, size, rng)

	return &Result{
		ObjectType:  inner.ObjectType,
		PrimaryKeys: sampled,
		Truncated:   inner.Truncated,
	}, nil
}

// reservoirSample returns a k-element uniform random sample of pks using
// Algorithm R. When k >= len(pks) the entire slice is returned as a fresh
// copy. Nil or empty pks returns an empty slice.
func reservoirSample(pks []string, k int, rng *rand.Rand) []string {
	if k <= 0 || len(pks) == 0 {
		return []string{}
	}
	if k >= len(pks) {
		out := make([]string, len(pks))
		copy(out, pks)
		return out
	}
	reservoir := make([]string, k)
	copy(reservoir, pks[:k])
	for i := k; i < len(pks); i++ {
		j := rng.Intn(i + 1)
		if j < k {
			reservoir[j] = pks[i]
		}
	}
	return reservoir
}

// executeReference looks up a stored ObjectSet from the Store and executes it.
func (e *Executor) executeReference(ctx context.Context, def *Definition) (*Result, error) {
	if e.store == nil {
		return nil, fmt.Errorf("objectSet store not available")
	}
	stored, err := e.store.Get(def.Reference)
	if err != nil {
		return nil, fmt.Errorf("resolve reference %q: %w", def.Reference, err)
	}
	return e.execute(ctx, stored)
}
