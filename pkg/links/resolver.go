package links

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// searcher is the minimal contract the resolver requires from an index source.
// It is satisfied by *index.Manager. Tests use a counting decorator to assert
// that a single batch query is issued instead of an N+1 loop.
type searcher interface {
	Search(objectType string, req *bleve.SearchRequest) (*bleve.SearchResult, error)
}

// Direction is the traversal direction for a link type.
// Forward: source -> target (existing behavior).
// Reverse: target -> source (new Phase 1 capability).
type Direction int

const (
	// DirectionForward traverses the link in its declared source -> target direction.
	DirectionForward Direction = iota
	// DirectionReverse traverses the link from target back to source.
	DirectionReverse
)

// String returns a stable lowercase string representation ("forward" / "reverse").
func (d Direction) String() string {
	if d == DirectionReverse {
		return "reverse"
	}
	return "forward"
}

// ParseDirection converts a string ("", "forward", "reverse") into a Direction.
// Empty string defaults to forward. Unknown values return an error.
func ParseDirection(s string) (Direction, error) {
	switch s {
	case "", "forward":
		return DirectionForward, nil
	case "reverse":
		return DirectionReverse, nil
	default:
		return DirectionForward, fmt.Errorf("invalid direction %q: must be \"forward\" or \"reverse\"", s)
	}
}

// LinkResolver resolves linked objects through link types.
type LinkResolver interface {
	// ResolveLinkedObjects finds objects linked to the given source objects through the specified link type.
	// sourcePKs are the primary keys of the source objects.
	// Returns primary keys of the linked (target) objects.
	// Legacy forward-only method.
	ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error)

	// ResolveLinkedObjectsByAPIName resolves links using the link type's API name and source object type.
	// Legacy forward-only method.
	ResolveLinkedObjectsByAPIName(ctx context.Context, sourceObjectTypeRID, linkTypeAPIName string, sourcePKs []string) ([]string, error)

	// ResolveLinked is the direction-aware overload. Legacy methods delegate here with DirectionForward.
	ResolveLinked(ctx context.Context, linkTypeRID string, pks []string, dir Direction) ([]string, error)
}

// FKConfig is the foreign key configuration for a link type.
type FKConfig struct {
	SourceProperty string `json:"sourceProperty"`
	TargetProperty string `json:"targetProperty"`
}

// Resolver implements LinkResolver using OMS repository and Bleve indexes.
// For M2M links, it also reads from the link_edges PostgreSQL table via the
// optional EdgeRepository.
//
// indexMgr is held as the local searcher interface so that the perf-counting
// test wrapper can be substituted in unit tests without dragging in the
// concrete *index.Manager.
type Resolver struct {
	repo     oms.Repository
	indexMgr searcher
	edgeRepo EdgeRepository
	ltCache  *LinkTypeCache
}

// SetLinkTypeCache installs a LinkTypeCache that the resolver will consult
// for link-type metadata lookups (GetLinkType / ListOutgoingLinkTypes) before
// delegating to the repository. Pass nil to disable caching.
func (r *Resolver) SetLinkTypeCache(c *LinkTypeCache) {
	r.ltCache = c
}

// NewResolver creates a new link resolver with FK-based resolution only.
// M2M resolution via join tables requires an EdgeRepository — use
// NewResolverWithEdges to enable it.
func NewResolver(repo oms.Repository, indexMgr *index.Manager) *Resolver {
	return &Resolver{
		repo:     repo,
		indexMgr: indexMgr,
	}
}

// NewResolverWithEdges creates a new link resolver that supports both FK and
// M2M join-table resolution.
func NewResolverWithEdges(repo oms.Repository, indexMgr *index.Manager, edgeRepo EdgeRepository) *Resolver {
	return &Resolver{
		repo:     repo,
		indexMgr: indexMgr,
		edgeRepo: edgeRepo,
	}
}

// newResolverWithSearcher is the internal constructor used by perf tests to
// inject a counting searcher decorator. It is unexported because production
// code should always go through NewResolver / NewResolverWithEdges with a
// real *index.Manager.
func newResolverWithSearcher(repo oms.Repository, s searcher) *Resolver {
	return &Resolver{
		repo:     repo,
		indexMgr: s,
	}
}

// ResolveLinkedObjects finds objects linked to the given source objects through the specified link type RID.
// Legacy forward-only method.
func (r *Resolver) ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	return r.ResolveLinked(ctx, linkTypeRID, sourcePKs, DirectionForward)
}

// ResolveLinkedObjectsByAPIName resolves links using the link type's API name.
// The source identifier accepts either an object type RID (legacy) or an API
// name; API names are translated to an RID via the ontology scope stamped on
// ctx (see index.WithOntologyScope) + oms.Repository.GetOntology /
// GetObjectTypeByAPIName. The dual-accept shape lets upstream callers
// (objectset executor, withProperties, searchAround) pass the friendlier
// apiname without knowing the per-ontology RID layout.
func (r *Resolver) ResolveLinkedObjectsByAPIName(ctx context.Context, sourceIdent, linkTypeAPIName string, sourcePKs []string) ([]string, error) {
	sourceRID, err := r.resolveSourceObjectTypeRID(ctx, sourceIdent)
	if err != nil {
		return nil, err
	}
	linkTypes, err := r.listOutgoingLinkTypes(ctx, sourceRID)
	if err != nil {
		return nil, fmt.Errorf("list link types: %w", err)
	}

	for _, lt := range linkTypes {
		if lt.APIName == linkTypeAPIName {
			return r.dispatch(ctx, &lt, sourcePKs, DirectionForward)
		}
	}

	return nil, fmt.Errorf("link type %q not found for source %q", linkTypeAPIName, sourceIdent)
}

// ResolveLinkedReverseByAPIName resolves a link by API name in reverse. The
// caller object type is the link's declared target; results are source-side PKs.
func (r *Resolver) ResolveLinkedReverseByAPIName(ctx context.Context, callerIdent, linkTypeAPIName string, callerPKs []string) ([]string, error) {
	callerRID, err := r.resolveSourceObjectTypeRID(ctx, callerIdent)
	if err != nil {
		return nil, err
	}
	linkTypes, err := r.repo.ListIncomingLinkTypes(ctx, callerRID)
	if err != nil {
		return nil, fmt.Errorf("list incoming link types: %w", err)
	}

	for _, lt := range linkTypes {
		if lt.APIName == linkTypeAPIName {
			return r.dispatch(ctx, &lt, callerPKs, DirectionReverse)
		}
	}

	return nil, fmt.Errorf("link type %q not found for target %q", linkTypeAPIName, callerIdent)
}

// ResolveTargetObjectType returns the target ObjectType API name for a forward
// link walk by API name.
func (r *Resolver) ResolveTargetObjectType(ctx context.Context, sourceIdent, linkTypeAPIName string) (string, error) {
	sourceRID, err := r.resolveSourceObjectTypeRID(ctx, sourceIdent)
	if err != nil {
		return "", err
	}
	linkTypes, err := r.listOutgoingLinkTypes(ctx, sourceRID)
	if err != nil {
		return "", fmt.Errorf("list link types: %w", err)
	}

	for _, lt := range linkTypes {
		if lt.APIName == linkTypeAPIName {
			return r.resolveObjectTypeAPIName(ctx, lt.TargetObjectType)
		}
	}

	return "", fmt.Errorf("link type %q not found for source %q", linkTypeAPIName, sourceIdent)
}

// ResolveTargetObjectTypeDir returns the ObjectType API name on the other end
// of a link walk in the requested direction.
func (r *Resolver) ResolveTargetObjectTypeDir(ctx context.Context, callerIdent, linkTypeAPIName string, dir Direction) (string, error) {
	if dir == DirectionForward {
		return r.ResolveTargetObjectType(ctx, callerIdent, linkTypeAPIName)
	}

	callerRID, err := r.resolveSourceObjectTypeRID(ctx, callerIdent)
	if err != nil {
		return "", err
	}
	linkTypes, err := r.repo.ListIncomingLinkTypes(ctx, callerRID)
	if err != nil {
		return "", fmt.Errorf("list incoming link types: %w", err)
	}

	for _, lt := range linkTypes {
		if lt.APIName == linkTypeAPIName {
			return r.resolveObjectTypeAPIName(ctx, lt.SourceObjectType)
		}
	}

	return "", fmt.Errorf("link type %q not found for target %q", linkTypeAPIName, callerIdent)
}

// resolveSourceObjectTypeRID normalises an object-type identifier that may be
// either an RID (starts with "ri.") or an APIName. API names are translated
// via the ontology scope on ctx: GetOntology(scope) → GetObjectTypeByAPIName.
// If either lookup fails the caller's original identifier is returned so the
// existing error path stays intact ("not found for source X").
func (r *Resolver) resolveSourceObjectTypeRID(ctx context.Context, sourceIdent string) (string, error) {
	if sourceIdent == "" || strings.HasPrefix(sourceIdent, "ri.") {
		return sourceIdent, nil
	}
	if r.repo == nil {
		return sourceIdent, nil
	}
	scope := index.OntologyScopeFromContext(ctx)
	if scope == "" {
		return sourceIdent, nil
	}
	ont, err := r.repo.GetOntology(ctx, scope)
	if err != nil || ont == nil {
		return sourceIdent, nil
	}
	ot, err := r.repo.GetObjectTypeByAPIName(ctx, ont.RID, sourceIdent)
	if err != nil || ot == nil {
		return sourceIdent, nil
	}
	return ot.RID, nil
}

func (r *Resolver) resolveObjectTypeAPIName(ctx context.Context, objectTypeIdent string) (string, error) {
	if objectTypeIdent == "" || !strings.HasPrefix(objectTypeIdent, "ri.") {
		return objectTypeIdent, nil
	}
	ot, err := r.repo.GetObjectType(ctx, objectTypeIdent)
	if err != nil {
		return "", fmt.Errorf("get object type %q: %w", objectTypeIdent, err)
	}
	return ot.APIName, nil
}

// ResolveLinked is the direction-aware overload.
// On DirectionReverse the caller's pks are treated as keys of the link's TARGET
// object type (or SOURCE for reverse of a forward traversal — see design doc).
func (r *Resolver) ResolveLinked(ctx context.Context, linkTypeRID string, pks []string, dir Direction) ([]string, error) {
	lt, err := r.getLinkType(ctx, linkTypeRID)
	if err != nil {
		return nil, fmt.Errorf("get link type: %w", err)
	}
	return r.dispatch(ctx, lt, pks, dir)
}

// getLinkType consults the optional LinkTypeCache before delegating to the
// repository. When no cache is configured this is a direct pass-through.
func (r *Resolver) getLinkType(ctx context.Context, rid string) (*oms.LinkType, error) {
	if r.ltCache != nil {
		return r.ltCache.GetLinkType(ctx, r.repo, rid)
	}
	return r.repo.GetLinkType(ctx, rid)
}

// listOutgoingLinkTypes consults the optional LinkTypeCache before delegating
// to the repository.
func (r *Resolver) listOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]oms.LinkType, error) {
	if r.ltCache != nil {
		return r.ltCache.ListOutgoingLinkTypes(ctx, r.repo, objectTypeRID)
	}
	return r.repo.ListOutgoingLinkTypes(ctx, objectTypeRID)
}

// dispatch routes to the appropriate resolution strategy based on link cardinality and direction.
// Phase 1 supports:
//   - FK forward (legacy resolveFK)
//   - M2M forward/reverse via link_edges table (resolveJoinTable)
//
// FK reverse and full M2M wiring into ObjectSet traversal land in Phase 2+.
func (r *Resolver) dispatch(ctx context.Context, lt *oms.LinkType, pks []string, dir Direction) ([]string, error) {
	// M2M: route to join-table resolver if available.
	if lt.Cardinality == "MANY_TO_MANY" {
		if r.edgeRepo == nil {
			return nil, fmt.Errorf("link type %q is MANY_TO_MANY but no edge repository is configured", lt.APIName)
		}
		return r.resolveJoinTable(ctx, lt, pks, dir)
	}

	// FK: currently forward-only. Reverse FK is a Phase 2 concern.
	if len(lt.ForeignKeyConfig) > 0 {
		if dir == DirectionReverse {
			return r.resolveFKReverse(ctx, lt, pks)
		}
		return r.resolveFK(ctx, lt, pks)
	}

	return nil, fmt.Errorf("link type %q has no usable resolver config (cardinality=%q)", lt.APIName, lt.Cardinality)
}

// parseFKConfig parses the foreign key configuration JSON.
func parseFKConfig(raw json.RawMessage) (*FKConfig, error) {
	var cfg FKConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
