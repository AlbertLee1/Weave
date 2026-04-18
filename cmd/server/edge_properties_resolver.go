package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oms"
)

// edgePropsResolverRepo is the narrow oms.Repository subset the edge-props
// adapter needs. Keeps the adapter independent of the full Repository — same
// pattern as interfaceResolverRepo.
type edgePropsResolverRepo interface {
	GetOntology(ctx context.Context, ridOrApiName string) (*oms.Ontology, error)
	GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiNameOrRID string) (*oms.ObjectType, error)
	ListOutgoingLinkTypes(ctx context.Context, objectTypeRID string) ([]oms.LinkType, error)
	ListIncomingLinkTypes(ctx context.Context, objectTypeRID string) ([]oms.LinkType, error)
}

// pgEdgePropertiesResolver satisfies objectset.EdgePropertiesProvider by
// looking up the LinkType from the request-scoped ontology and then asking
// the LinkEdgeStore for the per-edge properties keyed by the "other end" PK.
//
// For a searchAround walk in the forward direction, sourcePKs are the inner
// result's PKs and the returned map is keyed by the target PK. For a reverse
// walk, sourcePKs are *target* PKs and the map is keyed by source PK. This
// lets the handler surface edge_properties under the correct row without a
// second round-trip.
type pgEdgePropertiesResolver struct {
	repo      edgePropsResolverRepo
	edgeStore oms.LinkEdgeStore
}

func newPGEdgePropertiesResolver(repo edgePropsResolverRepo, edgeStore oms.LinkEdgeStore) *pgEdgePropertiesResolver {
	return &pgEdgePropertiesResolver{repo: repo, edgeStore: edgeStore}
}

// ResolveEdgeProperties implements objectset.EdgePropertiesProvider.
func (r *pgEdgePropertiesResolver) ResolveEdgeProperties(
	ctx context.Context,
	sourceObjectType, linkAPIName string,
	sourcePKs []string,
	dir links.Direction,
) (map[string]map[string]interface{}, error) {
	if r == nil || r.repo == nil || r.edgeStore == nil {
		return nil, nil
	}
	if len(sourcePKs) == 0 {
		return nil, nil
	}
	scope := index.OntologyScopeFromContext(ctx)
	if scope == "" {
		return nil, nil
	}
	ont, err := r.repo.GetOntology(ctx, scope)
	if err != nil || ont == nil {
		return nil, nil
	}
	ot, err := r.repo.GetObjectTypeByAPIName(ctx, ont.RID, sourceObjectType)
	if err != nil || ot == nil {
		return nil, nil
	}

	lt, err := r.findLinkType(ctx, ot.RID, linkAPIName, dir)
	if err != nil || lt == nil {
		return nil, err
	}
	if lt.Cardinality != "MANY_TO_MANY" {
		// Only M2M edges carry edge_properties.
		return nil, nil
	}

	edges, err := r.listEdges(ctx, lt.RID, sourcePKs, dir)
	if err != nil {
		return nil, err
	}
	if len(edges) == 0 {
		return nil, nil
	}
	out := make(map[string]map[string]interface{}, len(edges))
	for _, edge := range edges {
		if len(edge.EdgeProperties) == 0 {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal(edge.EdgeProperties, &m); err != nil {
			return nil, fmt.Errorf("decode edge_properties for %s/%s->%s: %w",
				edge.LinkTypeRID, edge.SourceObjectPK, edge.TargetObjectPK, err)
		}
		if len(m) == 0 {
			continue
		}
		key := edge.TargetObjectPK
		if dir == links.DirectionReverse {
			key = edge.SourceObjectPK
		}
		out[key] = m
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// findLinkType resolves a link by apiName under the given ObjectType RID,
// honouring direction: forward lookups scan outgoing links, reverse scan
// incoming ones (since sourceObjectType is the link's declared *target* then).
func (r *pgEdgePropertiesResolver) findLinkType(ctx context.Context, callerOTRID, linkAPIName string, dir links.Direction) (*oms.LinkType, error) {
	var lts []oms.LinkType
	var err error
	if dir == links.DirectionReverse {
		lts, err = r.repo.ListIncomingLinkTypes(ctx, callerOTRID)
	} else {
		lts, err = r.repo.ListOutgoingLinkTypes(ctx, callerOTRID)
	}
	if err != nil {
		return nil, err
	}
	for i := range lts {
		if lts[i].APIName == linkAPIName {
			return &lts[i], nil
		}
	}
	return nil, nil
}

// listEdges returns all link_edges rows for the given LinkType whose
// caller-side PK is in the input set. For forward direction the caller side
// is the source; for reverse it's the target (we match edges ending at the
// input PKs and key enrichment by the source PK).
func (r *pgEdgePropertiesResolver) listEdges(ctx context.Context, linkTypeRID string, pks []string, dir links.Direction) ([]oms.LinkEdge, error) {
	if dir == links.DirectionReverse {
		return r.edgeStore.ListLinkEdgesWithPropertiesByTarget(ctx, linkTypeRID, pks)
	}
	return r.edgeStore.ListLinkEdgesWithProperties(ctx, linkTypeRID, pks)
}
