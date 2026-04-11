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
