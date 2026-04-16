package e2e_test

import (
	"context"
	"encoding/json"

	"github.com/liyang/weave/pkg/oms"
)

// Branch stubs on inMemoryOmsRepo to satisfy oms.Repository.

func (r *inMemoryOmsRepo) CreateBranch(_ context.Context, _ *oms.OntologyBranch) error {
	return nil
}
func (r *inMemoryOmsRepo) GetBranch(_ context.Context, _ string) (*oms.OntologyBranch, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) ListBranches(_ context.Context, _ string) ([]oms.OntologyBranch, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) CloseBranch(_ context.Context, _ string) error {
	return nil
}
func (r *inMemoryOmsRepo) UpdateBranchStatus(_ context.Context, _, _ string) error {
	return nil
}
func (r *inMemoryOmsRepo) UpdateBranchBaseVersion(_ context.Context, _ string, _ int64) error {
	return nil
}
func (r *inMemoryOmsRepo) CreateBranchChange(_ context.Context, _ *oms.BranchChange) error {
	return nil
}
func (r *inMemoryOmsRepo) ListBranchChanges(_ context.Context, _ string) ([]oms.BranchChange, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) UpdateBranchChangeBeforeState(_ context.Context, _ string, _ json.RawMessage) error {
	return nil
}
