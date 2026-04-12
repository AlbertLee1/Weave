package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/security"
)

// policyProviderRepo is the narrow oms.Repository subset the executor-side
// policy provider needs. Kept local to cmd/server for the same reason the
// interfaceResolverRepo shim lives here — avoid bloating the 50+-method
// oms.Repository interface with consumer-specific methods.
type policyProviderRepo interface {
	GetOntology(ctx context.Context, ridOrApiName string) (*oms.Ontology, error)
	GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*oms.ObjectType, error)
}

// policyQueryAdapter implements pkg/oss/objectset.PolicyQueryProvider by
// translating the apiName passed in at executor time back into an
// ObjectType (via the ontology scope stamped onto the request context) and
// then forwarding to the shared *security.Engine. This is the single hook
// point that keeps the pkg/oss/objectset package free of a direct
// pkg/security import while still enforcing row-level policy on
// LoadObjectSet / Aggregate paths (US-046).
type policyQueryAdapter struct {
	repo   policyProviderRepo
	engine *security.Engine
}

func newPolicyQueryAdapter(repo policyProviderRepo, engine *security.Engine) *policyQueryAdapter {
	return &policyQueryAdapter{repo: repo, engine: engine}
}

// PolicyQuery satisfies the objectset.PolicyQueryProvider contract. A nil
// receiver or nil engine short-circuits to (nil, nil) which the executor
// treats as "no policy attached" and uses its base query unchanged.
func (a *policyQueryAdapter) PolicyQuery(ctx context.Context, objectType string) (query.Query, error) {
	if a == nil || a.engine == nil || a.repo == nil {
		return nil, nil
	}
	scope := index.OntologyScopeFromContext(ctx)
	if scope == "" {
		// Without ontology scope we cannot resolve the apiName → RID
		// mapping. Fail closed: return an error so the caller surfaces
		// the misconfiguration rather than silently returning all rows.
		return nil, errors.New("policy provider: no ontology scope on context")
	}
	ont, err := a.repo.GetOntology(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("policy provider: lookup ontology %q: %w", scope, err)
	}
	if ont == nil {
		return nil, fmt.Errorf("policy provider: ontology %q not found", scope)
	}
	ot, err := a.repo.GetObjectTypeByAPIName(ctx, ont.RID, objectType)
	if err != nil {
		return nil, fmt.Errorf("policy provider: lookup object type %q: %w", objectType, err)
	}
	if ot == nil {
		return nil, fmt.Errorf("policy provider: object type %q not found in ontology %q", objectType, scope)
	}
	user := auth.UserFromContext(ctx)
	return a.engine.Evaluate(ctx, user, *ot)
}
