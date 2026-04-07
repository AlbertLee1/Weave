package chinook_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Stub link-edge methods on inMemoryOmsRepo to satisfy oms.Repository. The
// chinook fixture tests do not exercise the M2M write path, so these
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

// Object history stubs — tier 2.3 added these interface methods.
func (r *inMemoryOmsRepo) InsertObjectHistory(_ context.Context, _ *oms.ObjectHistory) error {
	return nil
}

func (r *inMemoryOmsRepo) ListObjectHistory(_ context.Context, _, _ string, _ int) ([]oms.ObjectHistory, error) {
	return nil, nil
}

func (r *inMemoryOmsRepo) GetObjectVersionCount(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}

// ObjectEmbedding stubs (Tier 3.1) — chinook fixture tests do not
// exercise the vector path.
func (r *inMemoryOmsRepo) UpsertObjectEmbedding(_ context.Context, _ *oms.ObjectEmbedding) error {
	return nil
}

func (r *inMemoryOmsRepo) GetObjectEmbedding(_ context.Context, _, _, _ string) (*oms.ObjectEmbedding, error) {
	return nil, oms.ErrNotFound
}

func (r *inMemoryOmsRepo) FindNearestNeighbors(_ context.Context, _ string, _ []float32, _ int, _ string) ([]oms.NearestNeighborResult, error) {
	return nil, nil
}
