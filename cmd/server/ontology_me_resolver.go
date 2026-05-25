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
	return &auth.ResolvedOntology{RID: o.RID, APIName: o.APIName}, nil
}
