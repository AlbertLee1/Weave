package links_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Branch stubs on mockRepo to satisfy oms.Repository.

func (m *mockRepo) CreateBranch(_ context.Context, _ *oms.OntologyBranch) error {
	return nil
}
func (m *mockRepo) GetBranch(_ context.Context, _ string) (*oms.OntologyBranch, error) {
	return nil, oms.ErrNotFound
}
func (m *mockRepo) ListBranches(_ context.Context, _ string) ([]oms.OntologyBranch, error) {
	return nil, nil
}
func (m *mockRepo) CloseBranch(_ context.Context, _ string) error {
	return nil
}
func (m *mockRepo) UpdateBranchStatus(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockRepo) CreateBranchChange(_ context.Context, _ *oms.BranchChange) error {
	return nil
}
func (m *mockRepo) ListBranchChanges(_ context.Context, _ string) ([]oms.BranchChange, error) {
	return nil, nil
}
