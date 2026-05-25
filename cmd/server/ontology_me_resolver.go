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

// GetActionType implements auth.ActionTypeResolver for the round-103
// action-check handler. Same adapter struct now serves four interfaces
// — single resolution, listing, and action lookup — so cmd/server
// keeps the auth ↔ oms bridge to one struct.
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
