package actions

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Automation stubs on mockOmsRepo to satisfy oms.Repository.

func (m *mockOmsRepo) CreateAutomationRule(_ context.Context, _ *oms.AutomationRule) error {
	return nil
}
func (m *mockOmsRepo) GetAutomationRule(_ context.Context, _ string) (*oms.AutomationRule, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) ListAutomationRules(_ context.Context, _ string) ([]oms.AutomationRule, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateAutomationRule(_ context.Context, _ *oms.AutomationRule) error {
	return nil
}
func (m *mockOmsRepo) DeleteAutomationRule(_ context.Context, _ string) error { return nil }
func (m *mockOmsRepo) InsertExecution(_ context.Context, _ *oms.AutomationExecution) error {
	return nil
}
func (m *mockOmsRepo) ListExecutions(_ context.Context, _ string) ([]oms.AutomationExecution, error) {
	return nil, nil
}
