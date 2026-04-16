package oss_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Branch stubs on mockOmsRepo to satisfy oms.Repository.

func (m *mockOmsRepo) CreateBranch(_ context.Context, _ *oms.OntologyBranch) error {
	return nil
}
func (m *mockOmsRepo) GetBranch(_ context.Context, _ string) (*oms.OntologyBranch, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) ListBranches(_ context.Context, _ string) ([]oms.OntologyBranch, error) {
	return nil, nil
}
func (m *mockOmsRepo) CloseBranch(_ context.Context, _ string) error {
	return nil
}
func (m *mockOmsRepo) UpdateBranchStatus(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockOmsRepo) CreateBranchChange(_ context.Context, _ *oms.BranchChange) error {
	return nil
}
func (m *mockOmsRepo) ListBranchChanges(_ context.Context, _ string) ([]oms.BranchChange, error) {
	return nil, nil
}
