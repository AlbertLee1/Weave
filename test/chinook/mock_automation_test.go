package chinook_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Automation stubs on inMemoryOmsRepo to satisfy oms.Repository.

func (r *inMemoryOmsRepo) CreateAutomationRule(_ context.Context, _ *oms.AutomationRule) error {
	return nil
}
func (r *inMemoryOmsRepo) GetAutomationRule(_ context.Context, _ string) (*oms.AutomationRule, error) {
	return nil, oms.ErrNotFound
}
func (r *inMemoryOmsRepo) ListAutomationRules(_ context.Context, _ string) ([]oms.AutomationRule, error) {
	return nil, nil
}
func (r *inMemoryOmsRepo) UpdateAutomationRule(_ context.Context, _ *oms.AutomationRule) error {
	return nil
}
func (r *inMemoryOmsRepo) DeleteAutomationRule(_ context.Context, _ string) error { return nil }
func (r *inMemoryOmsRepo) InsertExecution(_ context.Context, _ *oms.AutomationExecution) error {
	return nil
}
func (r *inMemoryOmsRepo) ListExecutions(_ context.Context, _ string) ([]oms.AutomationExecution, error) {
	return nil, nil
}
