package oms_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Stub link-edge methods on mockRepo to satisfy oms.Repository. The handler
// tests do not exercise the M2M write path, so these return nil.
func (m *mockRepo) UpsertLinkEdge(_ context.Context, _ *oms.LinkEdge) error {
	return nil
}

func (m *mockRepo) DeleteLinkEdge(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockRepo) DeleteAllLinkEdgesForSource(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockRepo) GetLinkTypeByAPIName(_ context.Context, _, _ string) (*oms.LinkType, error) {
	return nil, oms.ErrNotFound
}

// Object history stubs are declared in handlers_test.go for mockRepo.

// ObjectEmbedding stubs (Tier 3.1) — handler tests do not exercise the
// vector path, so all three are no-ops.
func (m *mockRepo) UpsertObjectEmbedding(_ context.Context, _ *oms.ObjectEmbedding) error {
	return nil
}

func (m *mockRepo) GetObjectEmbedding(_ context.Context, _, _, _ string) (*oms.ObjectEmbedding, error) {
	return nil, oms.ErrNotFound
}

func (m *mockRepo) FindNearestNeighbors(_ context.Context, _ string, _ []float32, _ int, _ string) ([]oms.NearestNeighborResult, error) {
	return nil, nil
}
