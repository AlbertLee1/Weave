package main

import (
	"context"
	"errors"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// omsOntologyResolver adapts an oms.Repository onto auth.OntologyResolver
// for the round-95 per-ontology /me handler. The translation lives in
// cmd/server (not pkg/auth or pkg/oms) so neither package imports the
// other — pkg/auth.policy_evaluator already imports pkg/oms and adding
// the reverse direction would form a cycle.
type omsOntologyResolver struct {
	repo oms.Repository
}

func (r *omsOntologyResolver) GetOntology(ctx context.Context, apiNameOrRID string) (*auth.ResolvedOntology, error) {
	o, err := r.repo.GetOntology(ctx, apiNameOrRID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			return nil, auth.ErrOntologyNotFound
		}
		return nil, err
	}
	return &auth.ResolvedOntology{RID: o.RID, APIName: o.APIName, DisplayName: o.DisplayName}, nil
}

// GetQueryType implements auth.QueryTypeResolver for the round-113
// query-check handler. Adapter now serves SIX auth interfaces from
// one struct (rounds 95/97/99/103/105/113); the "single adapter,
// many narrow interfaces" pattern remains the cleanest bridge.
func (r *omsOntologyResolver) GetQueryType(ctx context.Context, ontologyRID, apiName string) (*auth.ResolvedQueryType, error) {
	qt, err := r.repo.GetQueryTypeByAPIName(ctx, ontologyRID, apiName)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			return nil, auth.ErrQueryTypeNotFound
		}
		return nil, err
	}
	return &auth.ResolvedQueryType{RID: qt.RID, APIName: qt.APIName}, nil
}

// GetObjectType implements auth.ObjectTypeResolver for the round-105
// object-check handler.
func (r *omsOntologyResolver) GetObjectType(ctx context.Context, ontologyRID, apiName string) (*auth.ResolvedObjectType, error) {
	ot, err := r.repo.GetObjectTypeByAPIName(ctx, ontologyRID, apiName)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			return nil, auth.ErrObjectTypeNotFound
		}
		return nil, err
	}
	return &auth.ResolvedObjectType{RID: ot.RID, APIName: ot.APIName}, nil
}

// GetActionType implements auth.ActionTypeResolver for the round-103
// action-check handler. Same adapter struct now serves five interfaces
// — single resolution, listing, action lookup, object lookup — so
// cmd/server keeps the auth ↔ oms bridge to one struct.
func (r *omsOntologyResolver) GetActionType(ctx context.Context, ontologyRID, apiNameOrRID string) (*auth.ResolvedActionType, error) {
	at, err := r.repo.GetActionTypeByAPIName(ctx, ontologyRID, apiNameOrRID)
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			return nil, auth.ErrActionTypeNotFound
		}
		return nil, err
	}
	return &auth.ResolvedActionType{RID: at.RID, APIName: at.APIName}, nil
}

// ListOntologies implements auth.OntologyLister for the round-99
// /api/v2/me/ontologies handler. Same adapter struct serves both
// the single-lookup and list paths so cmd/server stays free of
// duplicate plumbing.
func (r *omsOntologyResolver) ListOntologies(ctx context.Context) ([]auth.ResolvedOntology, error) {
	all, err := r.repo.ListOntologies(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]auth.ResolvedOntology, 0, len(all))
	for _, o := range all {
		out = append(out, auth.ResolvedOntology{
			RID: o.RID, APIName: o.APIName, DisplayName: o.DisplayName,
		})
	}
	return out, nil
}
