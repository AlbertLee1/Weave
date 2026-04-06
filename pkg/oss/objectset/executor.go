package objectset

import (
	"context"
	"encoding/json"
	"fmt"

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

// Executor evaluates ObjectSet definitions.
type Executor struct {
	indexMgr     *index.Manager
	linkResolver links.LinkResolver
	store        *Store
}

// NewExecutor creates a new ObjectSet executor.
func NewExecutor(indexMgr *index.Manager, linkResolver links.LinkResolver, store *Store) *Executor {
	return &Executor{
		indexMgr:     indexMgr,
		linkResolver: linkResolver,
		store:        store,
	}
}

// Result holds the execution result.
type Result struct {
	ObjectType  string   // the object type API name
	PrimaryKeys []string // the matching primary keys
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
		return nil, fmt.Errorf("nearestNeighbors not yet supported: requires vector index backend")
	case "withProperties":
		return e.executeWithProperties(ctx, def)
	default:
		return nil, fmt.Errorf("unknown objectSet type: %q", def.Type)
	}
}

// executeBase queries the Bleve index for ALL objects of the given type.
func (e *Executor) executeBase(ctx context.Context, def *Definition) (*Result, error) {
	searchReq := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
	searchReq.Size = 10000 // reasonable limit
	searchReq.Fields = []string{"*"}

	result, err := e.indexMgr.Search(def.ObjectType, searchReq)
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
	searchReq.Size = 10000
	result, err := e.indexMgr.Search(baseResult.ObjectType, searchReq)
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
	}, nil
}

// executeUnion executes all sub-ObjectSets and unions the results.
func (e *Executor) executeUnion(ctx context.Context, def *Definition) (*Result, error) {
	seen := make(map[string]bool)
	var allPKs []string
	var objectType string

	for _, sub := range def.ObjectSets {
		result, err := e.execute(ctx, sub)
		if err != nil {
			return nil, fmt.Errorf("execute union sub: %w", err)
		}
		if objectType == "" {
			objectType = result.ObjectType
		}
		for _, pk := range result.PrimaryKeys {
			if !seen[pk] {
				allPKs = append(allPKs, pk)
				seen[pk] = true
			}
		}
	}

	return &Result{ObjectType: objectType, PrimaryKeys: allPKs}, nil
}

// executeIntersect executes all sub-ObjectSets and intersects the results.
func (e *Executor) executeIntersect(ctx context.Context, def *Definition) (*Result, error) {
	var objectType string

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

	return &Result{ObjectType: objectType, PrimaryKeys: pks}, nil
}

// executeSubtract executes all sub-ObjectSets and subtracts subsequent sets from the first.
func (e *Executor) executeSubtract(ctx context.Context, def *Definition) (*Result, error) {
	// Execute the first sub-ObjectSet as the base
	firstResult, err := e.execute(ctx, def.ObjectSets[0])
	if err != nil {
		return nil, fmt.Errorf("execute subtract base: %w", err)
	}

	// Collect all PKs to exclude from subsequent sub-ObjectSets
	exclude := make(map[string]bool)
	for _, sub := range def.ObjectSets[1:] {
		result, err := e.execute(ctx, sub)
		if err != nil {
			return nil, fmt.Errorf("execute subtract sub: %w", err)
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

	return &Result{ObjectType: firstResult.ObjectType, PrimaryKeys: pks}, nil
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
	}, nil
}

// reverseLinkFinder is an optional interface that resolvers implement to
// expose reverse-traversal-by-API-name for the ObjectSet executor. It is
// separate from LinkResolver so reverse support remains opt-in.
type reverseLinkFinder interface {
	ResolveLinkedReverseByAPIName(ctx context.Context, callerObjectType, linkTypeAPIName string, callerPKs []string) ([]string, error)
}

// executeWithProperties executes the inner ObjectSet — property filtering happens at the response layer.
func (e *Executor) executeWithProperties(ctx context.Context, def *Definition) (*Result, error) {
	return e.execute(ctx, def.ObjectSet)
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
