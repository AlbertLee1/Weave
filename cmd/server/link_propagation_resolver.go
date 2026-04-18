package main

import (
	"context"
	"errors"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// linkPropagationResolverRepo is the narrow oms.Repository subset the
// linkPropagationResolver needs. Local interface keeps the test stubs tiny
// — same shape as interfaceResolverRepo / edge property resolver.
type linkPropagationResolverRepo interface {
	GetLinkType(ctx context.Context, rid string) (*oms.LinkType, error)
	GetObjectType(ctx context.Context, rid string) (*oms.ObjectType, error)
}

// linkPropagationResolver adapts the OMS repo to the narrow
// funnel.LinkPropagationResolver interface used by the US-261 marking
// inheritance hook. Kept in cmd/server so pkg/funnel stays free of any
// LinkType -> ObjectType API-name resolution logic, matching the same
// "narrow interface in pkg, concrete adapter in cmd/server" shape used by
// pgEdgePropertiesResolver and interfaceMethodActionDispatcher.
type linkPropagationResolver struct {
	repo linkPropagationResolverRepo
}

func newLinkPropagationResolver(repo linkPropagationResolverRepo) *linkPropagationResolver {
	return &linkPropagationResolver{repo: repo}
}

// LookupLinkPropagation returns the LinkType's PropagateMarkings flag plus
// the source/target ObjectType API names so the funnel consumer can scope
// per-objectType Bleve fetches/writes. Returns (zero, false, nil) when the
// LinkType row is missing (treated as a soft skip — the consumer logs and
// moves on so a stale or out-of-band-deleted LinkType cannot poison the
// stream). LinkType.{Source,Target}ObjectType carries an ObjectType RID;
// the resolver swaps it for the API name (the Bleve scoped-key form) via
// repo.GetObjectType.
func (r *linkPropagationResolver) LookupLinkPropagation(ctx context.Context, linkTypeRID string) (funnel.LinkPropagation, bool, error) {
	if r == nil || r.repo == nil || linkTypeRID == "" {
		return funnel.LinkPropagation{}, false, nil
	}
	lt, err := r.repo.GetLinkType(ctx, linkTypeRID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			return funnel.LinkPropagation{}, false, nil
		}
		return funnel.LinkPropagation{}, false, err
	}
	srcAPIName, err := r.objectTypeAPIName(ctx, lt.SourceObjectType)
	if err != nil {
		return funnel.LinkPropagation{}, false, err
	}
	tgtAPIName, err := r.objectTypeAPIName(ctx, lt.TargetObjectType)
	if err != nil {
		return funnel.LinkPropagation{}, false, err
	}
	return funnel.LinkPropagation{
		PropagateMarkings:       lt.PropagateMarkings,
		SourceObjectTypeAPIName: srcAPIName,
		TargetObjectTypeAPIName: tgtAPIName,
	}, true, nil
}

// objectTypeAPIName resolves an ObjectType RID to the api_name that the
// Bleve index manager keys on. A missing ObjectType returns "" + nil so
// callers can short-circuit cleanly without bubbling the lookup error.
func (r *linkPropagationResolver) objectTypeAPIName(ctx context.Context, otRID string) (string, error) {
	if otRID == "" {
		return "", nil
	}
	ot, err := r.repo.GetObjectType(ctx, otRID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return ot.APIName, nil
}
