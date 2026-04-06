package links

import (
	"context"
	"fmt"

	"github.com/liyang/weave/pkg/oms"
)

// EdgeRepository is the data-access contract for the M2M link_edges table.
// It is a separate interface from oms.Repository so that the link resolver
// can be wired with or without M2M support, and so the in-memory test fixtures
// in pkg/links can mock edges without implementing the full PG behaviour.
type EdgeRepository interface {
	// ListEdgeTargets returns the target_object_rid values for edges where the
	// link type matches and the source_object_rid is in the given set.
	// Results are deduplicated and caller-ordered by appearance.
	ListEdgeTargets(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error)

	// ListEdgeSources returns the source_object_rid values for edges where the
	// link type matches and the target_object_rid is in the given set.
	// Used for reverse M2M traversal.
	ListEdgeSources(ctx context.Context, linkTypeRID string, targetPKs []string) ([]string, error)
}

// ResolveViaJoinTable is a package-level helper that executes an M2M lookup
// against an EdgeRepository. It is exposed for tests and for direct callers
// who hold an EdgeRepository but not a full Resolver.
//
// Deprecated in favour of (*Resolver).resolveJoinTable for most callers —
// kept exported as a standalone function for testability per the design doc.
func ResolveViaJoinTable(ctx context.Context, edgeRepo EdgeRepository, lt *oms.LinkType, pks []string, dir Direction) ([]string, error) {
	if edgeRepo == nil {
		return nil, fmt.Errorf("ResolveViaJoinTable: nil edge repository")
	}
	if lt == nil {
		return nil, fmt.Errorf("ResolveViaJoinTable: nil link type")
	}
	if lt.Cardinality != "MANY_TO_MANY" {
		return nil, fmt.Errorf("ResolveViaJoinTable: link type %q is not MANY_TO_MANY (got %q)", lt.APIName, lt.Cardinality)
	}
	if len(pks) == 0 {
		return nil, nil
	}

	if dir == DirectionReverse {
		return edgeRepo.ListEdgeSources(ctx, lt.RID, pks)
	}
	return edgeRepo.ListEdgeTargets(ctx, lt.RID, pks)
}

// resolveJoinTable is the resolver-bound M2M strategy. It uses the configured
// EdgeRepository to look up edges in the shared link_edges table.
func (r *Resolver) resolveJoinTable(ctx context.Context, lt *oms.LinkType, pks []string, dir Direction) ([]string, error) {
	return ResolveViaJoinTable(ctx, r.edgeRepo, lt, pks, dir)
}
