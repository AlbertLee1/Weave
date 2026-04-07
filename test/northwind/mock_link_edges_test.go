package northwind_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Stub link-edge methods on inMemoryOmsRepo to satisfy oms.Repository. The
// northwind fixture tests do not exercise the M2M write path, so these
// return nil.
func (r *inMemoryOmsRepo) UpsertLinkEdge(_ context.Context, _ *oms.LinkEdge) error {
	return nil
}

func (r *inMemoryOmsRepo) DeleteLinkEdge(_ context.Context, _, _, _ string) error {
	return nil
}

func (r *inMemoryOmsRepo) DeleteAllLinkEdgesForSource(_ context.Context, _, _ string) error {
	return nil
}

func (r *inMemoryOmsRepo) GetLinkTypeByAPIName(_ context.Context, _, _ string) (*oms.LinkType, error) {
	return nil, oms.ErrNotFound
}
