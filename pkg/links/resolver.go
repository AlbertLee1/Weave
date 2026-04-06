package links

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

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
type Resolver struct {
	repo     oms.Repository
	indexMgr *index.Manager
	edgeRepo EdgeRepository
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

// ResolveLinkedObjects finds objects linked to the given source objects through the specified link type RID.
// Legacy forward-only method.
func (r *Resolver) ResolveLinkedObjects(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	return r.ResolveLinked(ctx, linkTypeRID, sourcePKs, DirectionForward)
}

// ResolveLinkedObjectsByAPIName resolves links using the link type's API name and source object type RID.
// Legacy forward-only method.
func (r *Resolver) ResolveLinkedObjectsByAPIName(ctx context.Context, sourceObjectTypeRID, linkTypeAPIName string, sourcePKs []string) ([]string, error) {
	linkTypes, err := r.repo.ListOutgoingLinkTypes(ctx, sourceObjectTypeRID)
	if err != nil {
		return nil, fmt.Errorf("list link types: %w", err)
	}

	for _, lt := range linkTypes {
		if lt.APIName == linkTypeAPIName {
			return r.dispatch(ctx, &lt, sourcePKs, DirectionForward)
		}
	}

	return nil, fmt.Errorf("link type %q not found for source %q", linkTypeAPIName, sourceObjectTypeRID)
}

// ResolveLinked is the direction-aware overload.
// On DirectionReverse the caller's pks are treated as keys of the link's TARGET
// object type (or SOURCE for reverse of a forward traversal — see design doc).
func (r *Resolver) ResolveLinked(ctx context.Context, linkTypeRID string, pks []string, dir Direction) ([]string, error) {
	lt, err := r.repo.GetLinkType(ctx, linkTypeRID)
	if err != nil {
		return nil, fmt.Errorf("get link type: %w", err)
	}
	return r.dispatch(ctx, lt, pks, dir)
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
