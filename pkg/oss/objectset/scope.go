package objectset

import (
	"context"

	"github.com/liyang/weave/pkg/index"
)

// WithOntologyScope is a thin re-export of index.WithOntologyScope so handler
// code in this package can stamp the scope without importing pkg/index just
// for the helper. Kept here so the call sites read naturally
// ("objectset.WithOntologyScope(ctx, ...)").
func WithOntologyScope(ctx context.Context, ontologyAPIName string) context.Context {
	return index.WithOntologyScope(ctx, ontologyAPIName)
}

// scopedIndexKey delegates to index.KeyForCtx so the executor and handlers in
// this package use the same fallback semantics as the rest of the codebase.
func scopedIndexKey(ctx context.Context, mgr *index.Manager, objectType string) string {
	return index.KeyForCtx(ctx, mgr, objectType)
}

// OntologyScopeFromContextOrEmpty returns the ontology API name stamped on
// ctx via WithOntologyScope, or "" when no scope is set. Exposed so the
// nearestNeighbors executor can pass the ontology through to the vector
// store without dragging pkg/index into its own package surface.
func OntologyScopeFromContextOrEmpty(ctx context.Context) string {
	return index.OntologyScopeFromContext(ctx)
}

// DefaultBranch is the canonical "main" branch name used when ?branch= is
// omitted from a LoadObjectSet request (US-381). All read paths that have
// not been explicitly opted into a branch overlay should resolve to this
// constant so the data plane stays in lockstep with the schema-branch
// machinery in pkg/oms.
const DefaultBranch = "main"

type branchScopeKey struct{}

// WithBranchScope stamps the requested branch onto ctx so the executor,
// HistorySnapshotProvider, and BranchScopeProvider implementations downstream
// can opt into branch-aware behaviour without an extra parameter on every
// call. Empty input or DefaultBranch is a no-op so the legacy main-only path
// stays free of context churn.
func WithBranchScope(ctx context.Context, branch string) context.Context {
	if branch == "" || branch == DefaultBranch {
		return ctx
	}
	return context.WithValue(ctx, branchScopeKey{}, branch)
}

// BranchScopeFromContext returns the branch stamped via WithBranchScope.
// Returns DefaultBranch ("main") when no branch was set so call sites can
// treat the value as authoritative without nil/empty guards.
func BranchScopeFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(branchScopeKey{}).(string); ok && v != "" {
		return v
	}
	return DefaultBranch
}
