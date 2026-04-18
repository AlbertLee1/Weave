package actions

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Stub object-embedding methods on mockOmsRepo to satisfy oms.Repository.
// The action tests do not exercise the pgvector kNN path, so these return
// zero values. Kept in a separate file from mock_link_edges_test.go so each
// interface-evolution add-on lives in one place.
func (m *mockOmsRepo) UpsertObjectEmbedding(_ context.Context, _ *oms.ObjectEmbedding) error {
	return nil
}

func (m *mockOmsRepo) GetObjectEmbedding(_ context.Context, _, _, _ string) (*oms.ObjectEmbedding, error) {
	return nil, nil
}

func (m *mockOmsRepo) FindNearestNeighbors(_ context.Context, _ string, _ []float32, _ int, _ string) ([]oms.NearestNeighborResult, error) {
	return nil, nil
}

// Function stubs — the action tests do not exercise the Function path.
func (m *mockOmsRepo) CreateFunction(_ context.Context, _ *oms.Function) error { return nil }
func (m *mockOmsRepo) GetFunction(_ context.Context, _ string) (*oms.Function, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) GetFunctionByName(_ context.Context, _, _ string) (*oms.Function, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) GetFunctionByNameVersion(_ context.Context, _, _, _ string) (*oms.Function, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) ListFunctions(_ context.Context, _ string) ([]oms.Function, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListFunctionVersionsByName(_ context.Context, _, _ string) ([]oms.Function, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateFunction(_ context.Context, _ *oms.Function) error { return nil }
func (m *mockOmsRepo) DeleteFunction(_ context.Context, _ string) error        { return nil }
