package oms_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Automation stubs on mockRepo to satisfy oms.Repository.

func (m *mockRepo) CreateAutomationRule(_ context.Context, _ *oms.AutomationRule) error { return nil }
func (m *mockRepo) GetAutomationRule(_ context.Context, _ string) (*oms.AutomationRule, error) {
	return nil, oms.ErrNotFound
}
func (m *mockRepo) ListAutomationRules(_ context.Context, _ string) ([]oms.AutomationRule, error) {
	return nil, nil
}
func (m *mockRepo) UpdateAutomationRule(_ context.Context, _ *oms.AutomationRule) error { return nil }
func (m *mockRepo) DeleteAutomationRule(_ context.Context, _ string) error              { return nil }
func (m *mockRepo) InsertExecution(_ context.Context, _ *oms.AutomationExecution) error { return nil }
func (m *mockRepo) ListExecutions(_ context.Context, _ string) ([]oms.AutomationExecution, error) {
	return nil, nil
}

// Automation stubs on noopRepo to satisfy oms.Repository.

func (n *noopRepo) CreateAutomationRule(_ context.Context, _ *oms.AutomationRule) error { return nil }
func (n *noopRepo) GetAutomationRule(_ context.Context, _ string) (*oms.AutomationRule, error) {
	return nil, nil
}
func (n *noopRepo) ListAutomationRules(_ context.Context, _ string) ([]oms.AutomationRule, error) {
	return nil, nil
}
func (n *noopRepo) UpdateAutomationRule(_ context.Context, _ *oms.AutomationRule) error { return nil }
func (n *noopRepo) DeleteAutomationRule(_ context.Context, _ string) error              { return nil }
func (n *noopRepo) InsertExecution(_ context.Context, _ *oms.AutomationExecution) error { return nil }
func (n *noopRepo) ListExecutions(_ context.Context, _ string) ([]oms.AutomationExecution, error) {
	return nil, nil
}
