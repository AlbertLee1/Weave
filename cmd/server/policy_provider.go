package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/rls"
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
	repo      policyProviderRepo
	engine    *security.Engine
	rlsEngine *rls.Engine
}

func newPolicyQueryAdapter(repo policyProviderRepo, engine *security.Engine) *policyQueryAdapter {
	return &policyQueryAdapter{repo: repo, engine: engine}
}

// SetRowPolicyEngine attaches the US-256 row_policies engine. The adapter
// compiles both the security.Engine output AND the rls.Engine output and
// AND-combines them before handing the result to the ObjectSet executor.
func (a *policyQueryAdapter) SetRowPolicyEngine(e *rls.Engine) {
	a.rlsEngine = e
}

// propertyFilterAdapter implements pkg/oss/objectset.PropertyFilterProvider
// by resolving apiName → ObjectType (via the ontology scope on context)
// and forwarding to *security.Engine.AllowedProperties. Lives alongside
// policyQueryAdapter because it shares the same narrow oms.Repository
// subset and the same ontology-scope context contract — keeping both
// adapters in one file makes the cmd/server ↔ pkg/security wiring
// obvious for future US-05x stories.
type propertyFilterAdapter struct {
	repo   policyProviderRepo
	engine *security.Engine
}

func newPropertyFilterAdapter(repo policyProviderRepo, engine *security.Engine) *propertyFilterAdapter {
	return &propertyFilterAdapter{repo: repo, engine: engine}
}

// AllowedProperties satisfies objectset.PropertyFilterProvider. A nil
// receiver or nil engine short-circuits to (nil, nil) which the handler
// treats as "no PROPERTY-scope policy attached" and passes the full
// property payload through unchanged. Errors surface when the ontology
// scope is missing or the apiName cannot be resolved, so misconfiguration
// fails loud rather than silently exposing column-level-secret fields.
func (a *propertyFilterAdapter) AllowedProperties(ctx context.Context, objectType string) ([]string, error) {
	if a == nil || a.engine == nil || a.repo == nil {
		return nil, nil
	}
	scope := index.OntologyScopeFromContext(ctx)
	if scope == "" {
		return nil, errors.New("property filter: no ontology scope on context")
	}
	ont, err := a.repo.GetOntology(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("property filter: lookup ontology %q: %w", scope, err)
	}
	if ont == nil {
		return nil, fmt.Errorf("property filter: ontology %q not found", scope)
	}
	ot, err := a.repo.GetObjectTypeByAPIName(ctx, ont.RID, objectType)
	if err != nil {
		return nil, fmt.Errorf("property filter: lookup object type %q: %w", objectType, err)
	}
	if ot == nil {
		return nil, fmt.Errorf("property filter: object type %q not found in ontology %q", objectType, scope)
	}
	user := auth.UserFromContext(ctx)
	return a.engine.AllowedProperties(ctx, user, *ot), nil
}

// ingestPolicyAdapter implements pkg/oss.IngestPolicyChecker by resolving
// the ontology + objectType API names to OMS types and delegating to
// *security.Engine.AllowedForIngest. This keeps the pkg/oss handler free
// of a direct pkg/security import (US-062).
type ingestPolicyAdapter struct {
	repo   policyProviderRepo
	engine *security.Engine
}

func newIngestPolicyAdapter(repo policyProviderRepo, engine *security.Engine) *ingestPolicyAdapter {
	return &ingestPolicyAdapter{repo: repo, engine: engine}
}

// AllowedForIngest satisfies oss.IngestPolicyChecker. A nil receiver or nil
// engine short-circuits to (true, nil) so pre-US-062 deployments where the
// policy engine is not attached still allow ingest (RBAC guards the route).
func (a *ingestPolicyAdapter) AllowedForIngest(ctx context.Context, ontologyAPIName, objectType string) (bool, error) {
	if a == nil || a.engine == nil || a.repo == nil {
		return true, nil
	}
	ont, err := a.repo.GetOntology(ctx, ontologyAPIName)
	if err != nil {
		return false, fmt.Errorf("ingest policy: lookup ontology %q: %w", ontologyAPIName, err)
	}
	if ont == nil {
		return false, fmt.Errorf("ingest policy: ontology %q not found", ontologyAPIName)
	}
	ot, err := a.repo.GetObjectTypeByAPIName(ctx, ont.RID, objectType)
	if err != nil {
		return false, fmt.Errorf("ingest policy: lookup object type %q: %w", objectType, err)
	}
	if ot == nil {
		return false, fmt.Errorf("ingest policy: object type %q not found in ontology %q", objectType, ontologyAPIName)
	}
	user := auth.UserFromContext(ctx)
	return a.engine.AllowedForIngest(ctx, user, *ot)
}

// PolicyQuery satisfies the objectset.PolicyQueryProvider contract. A nil
// receiver or no wired engines short-circuit to (nil, nil) which the
// executor treats as "no policy attached" and uses its base query unchanged.
func (a *policyQueryAdapter) PolicyQuery(ctx context.Context, objectType string) (query.Query, error) {
	if a == nil || a.repo == nil {
		return nil, nil
	}
	if a.engine == nil && a.rlsEngine == nil {
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

	var abacQ query.Query
	if a.engine != nil {
		q, err := a.engine.Evaluate(ctx, user, *ot)
		if err != nil {
			return nil, err
		}
		if _, isAll := q.(*query.MatchAllQuery); !isAll {
			abacQ = q
		}
	}

	var rlsQ query.Query
	if a.rlsEngine != nil {
		q, err := a.rlsEngine.Compile(ctx, user, ot.RID)
		if err != nil {
			return nil, err
		}
		rlsQ = q
	}

	switch {
	case abacQ == nil && rlsQ == nil:
		return bleve.NewMatchAllQuery(), nil
	case abacQ != nil && rlsQ == nil:
		return abacQ, nil
	case abacQ == nil && rlsQ != nil:
		return rlsQ, nil
	default:
		return bleve.NewConjunctionQuery(abacQ, rlsQ), nil
	}
}
