package objectset

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oss/where"
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

// Executor evaluates ObjectSet definitions.
type Executor struct {
	indexMgr       *index.Manager
	linkResolver   links.LinkResolver
	store          *Store
	interfaceResol InterfaceResolver
	vectorStore    NNVectorStore
	embedProvider  NNEmbeddingProvider
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

// BaseExecutionCap is the maximum number of primary keys executeBase will
// return from a single Bleve query. When a result hits this cap the Result's
// Truncated flag is set so callers can warn the user that the answer is
// approximate.
const BaseExecutionCap = 10000

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
}

// Execute evaluates an ObjectSet definition and returns matching primary keys.
func (e *Executor) Execute(ctx context.Context, def *Definition) (*Result, error) {
	if err := def.Validate(); err != nil {
		return nil, err
	}
	return e.execute(ctx, def)
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
	case "methodInput":
		return nil, fmt.Errorf("methodInput objectSet not yet supported: bind at function invocation time")
	default:
		return nil, fmt.Errorf("unknown objectSet type: %q", def.Type)
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
// via the InterfaceResolver, then queries each type's Bleve index for all
// objects. A nil resolver returns an error.
func (e *Executor) executeInterfaceBase(ctx context.Context, def *Definition) (*Result, error) {
	if e.interfaceResol == nil {
		return nil, fmt.Errorf("interfaceBase: interface resolver not configured")
	}
	types, err := e.interfaceResol.ResolveInterfaceObjectTypes(ctx, def.InterfaceType)
	if err != nil {
		return nil, fmt.Errorf("interfaceBase resolve %q: %w", def.InterfaceType, err)
	}

	seen := make(map[string]bool)
	var allPKs []string
	truncated := false
	for _, objType := range types {
		sub, err := e.executeBase(ctx, &Definition{Type: "base", ObjectType: objType})
		if err != nil {
			return nil, fmt.Errorf("interfaceBase %s: %w", objType, err)
		}
		if sub.Truncated {
			truncated = true
		}
		for _, pk := range sub.PrimaryKeys {
			if !seen[pk] {
				seen[pk] = true
				allPKs = append(allPKs, pk)
			}
		}
	}

	return &Result{
		// Tag the result with the interface API name so downstream callers
		// know this is a polymorphic set. Concrete per-type execution happens
		// at the handler layer for interface data query endpoints (US-026).
		ObjectType:  def.InterfaceType,
		PrimaryKeys: allPKs,
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
func (e *Executor) executeBase(ctx context.Context, def *Definition) (*Result, error) {
	searchReq := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	searchReq.Size = BaseExecutionCap
	searchReq.Fields = []string{"*"}

	result, err := e.indexMgr.Search(scopedIndexKey(ctx, e.indexMgr, def.ObjectType), searchReq)
	if err != nil {
		return nil, fmt.Errorf("search base objectSet %q: %w", def.ObjectType, err)
	}

	pks := make([]string, 0, len(result.Hits))
	for _, hit := range result.Hits {
		pks = append(pks, hit.ID)
	}

	return &Result{
		ObjectType:  def.ObjectType,
		PrimaryKeys: pks,
		Truncated:   len(pks) >= BaseExecutionCap,
	}, nil
}

// executeFilter executes the base objectSet, then applies the where clause filter.
func (e *Executor) executeFilter(ctx context.Context, def *Definition) (*Result, error) {
	baseResult, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, fmt.Errorf("execute filter base: %w", err)
	}

	// Parse the where clause
	var clause where.WhereClause
	if err := json.Unmarshal(def.Where, &clause); err != nil {
		return nil, fmt.Errorf("parse where clause: %w", err)
	}

	// Convert to Bleve query
	bleveQuery, err := where.ConvertToBleveQuery(&clause)
	if err != nil {
		return nil, fmt.Errorf("convert where clause: %w", err)
	}

	// Build a query that intersects the base PKs with the where filter.
	// Use DocIDQuery to limit to the base result PKs, combined with the where filter.
	docIDQ := bleve.NewDocIDQuery(baseResult.PrimaryKeys)
	conjQ := bleve.NewConjunctionQuery(docIDQ, bleveQuery)

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
func (e *Executor) executeSearchAround(ctx context.Context, def *Definition) (*Result, error) {
	sourceResult, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, fmt.Errorf("execute searchAround source: %w", err)
	}

	dir, err := links.ParseDirection(def.Direction)
	if err != nil {
		return nil, fmt.Errorf("searchAround direction: %w", err)
	}

	// Forward: use legacy API name path (unchanged behaviour).
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

	return &Result{
		ObjectType:  targetType,
		PrimaryKeys: linkedPKs,
		// Inherit truncation from the source set: if the inner set was already
		// approximate, the searchAround output is also approximate.
		Truncated: sourceResult.Truncated,
	}, nil
}

// reverseLinkFinder is an optional interface that resolvers implement to
// expose reverse-traversal-by-API-name for the ObjectSet executor. It is
// separate from LinkResolver so reverse support remains opt-in.
type reverseLinkFinder interface {
	ResolveLinkedReverseByAPIName(ctx context.Context, callerObjectType, linkTypeAPIName string, callerPKs []string) ([]string, error)
}

// executeWithProperties executes the inner ObjectSet, then evaluates each
// declared derived property as a single-hop link aggregation per base object.
// When no DerivedProperties are declared the call degenerates to the inner
// ObjectSet so pre-existing callers that only used Properties keep working.
//
// Scope: count / sum / avg / min / max over forward direction. Reverse
// direction lands in US-003 and intentionally returns an explicit error so
// the unsupported surface is obvious.
func (e *Executor) executeWithProperties(ctx context.Context, def *Definition) (*Result, error) {
	inner, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, err
	}
	if len(def.DerivedProperties) == 0 {
		return inner, nil
	}

	derived := make(map[string]map[string]interface{}, len(inner.PrimaryKeys))
	for _, pk := range inner.PrimaryKeys {
		derived[pk] = make(map[string]interface{}, len(def.DerivedProperties))
	}

	for _, dp := range def.DerivedProperties {
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
			if dir != links.DirectionForward {
				return nil, fmt.Errorf("withProperties %q: reverse direction numeric metrics not yet supported", dp.Name)
			}
			if err := e.evaluateNumericDerived(ctx, inner, dp, derived); err != nil {
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

// evaluateNumericDerived computes sum / avg / min / max of dp.Field across
// forward-linked target objects for every base PK in inner. The target
// ObjectType is resolved via LinkTargetTypeResolver so the numeric field can
// be read from the correct Bleve index. Non-numeric fields surface as
// DerivedPropertyTypeMismatch.
func (e *Executor) evaluateNumericDerived(ctx context.Context, inner *Result, dp DerivedPropertyDef, derived map[string]map[string]interface{}) error {
	targetType := ""
	if resolver, ok := e.linkResolver.(LinkTargetTypeResolver); ok {
		tt, err := resolver.ResolveTargetObjectType(ctx, inner.ObjectType, dp.Link)
		if err != nil {
			return fmt.Errorf("withProperties %q resolve target type for link %q: %w", dp.Name, dp.Link, err)
		}
		targetType = tt
	}
	if targetType == "" {
		return fmt.Errorf("withProperties %q: link resolver cannot determine target ObjectType for link %q", dp.Name, dp.Link)
	}
	targetIndexKey := scopedIndexKey(ctx, e.indexMgr, targetType)

	for _, pk := range inner.PrimaryKeys {
		targets, err := e.linkResolver.ResolveLinkedObjectsByAPIName(ctx, inner.ObjectType, dp.Link, []string{pk})
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
