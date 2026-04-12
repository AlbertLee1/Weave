package oss_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// This file exists solely to add compile-time stubs for the
// pgvector-backed Repository methods (Tier 3.1) to the various mock
// repositories used by other test files in this package. The stubs are
// intentionally additive: they keep the parallel-worker test fixtures
// (mockOmsRepo, etc.) compiling against the latest oms.Repository
// interface without touching their core definitions.
//
// The stub methods always return zero values; tests that exercise vector
// search live elsewhere with their own mocks.

func (m *mockOmsRepo) UpsertObjectEmbedding(_ context.Context, _ *oms.ObjectEmbedding) error {
	return nil
}
func (m *mockOmsRepo) GetObjectEmbedding(_ context.Context, _, _, _ string) (*oms.ObjectEmbedding, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) FindNearestNeighbors(_ context.Context, _ string, _ []float32, _ int, _ string) ([]oms.NearestNeighborResult, error) {
	return nil, nil
}

// Function stubs — OSS tests do not exercise the Function path.
func (m *mockOmsRepo) CreateFunction(_ context.Context, _ *oms.Function) error { return nil }
func (m *mockOmsRepo) GetFunction(_ context.Context, _ string) (*oms.Function, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) GetFunctionByName(_ context.Context, _, _ string) (*oms.Function, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) ListFunctions(_ context.Context, _ string) ([]oms.Function, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateFunction(_ context.Context, _ *oms.Function) error { return nil }
func (m *mockOmsRepo) DeleteFunction(_ context.Context, _ string) error        { return nil }
