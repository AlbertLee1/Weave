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
