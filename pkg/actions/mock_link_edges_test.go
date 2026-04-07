package actions

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Stub link-edge methods on mockOmsRepo to satisfy oms.Repository. The
// action tests do not exercise the M2M write path, so these return nil.
func (m *mockOmsRepo) UpsertLinkEdge(_ context.Context, _ *oms.LinkEdge) error {
	return nil
}

func (m *mockOmsRepo) DeleteLinkEdge(_ context.Context, _, _, _ string) error {
	return nil
}

func (m *mockOmsRepo) DeleteAllLinkEdgesForSource(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockOmsRepo) GetLinkTypeByAPIName(_ context.Context, _, _ string) (*oms.LinkType, error) {
	return nil, oms.ErrNotFound
}

// Object history stubs are declared in actions_test.go for mockOmsRepo.
