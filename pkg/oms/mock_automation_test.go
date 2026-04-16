package oms_test

import (
	"context"

	"github.com/liyang/weave/pkg/oms"
)

// Functional automation mock implementations on mockRepo.

func (m *mockRepo) CreateAutomationRule(_ context.Context, rule *oms.AutomationRule) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.automationRules = append(m.automationRules, *rule)
	return nil
}

func (m *mockRepo) GetAutomationRule(_ context.Context, id string) (*oms.AutomationRule, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	for i := range m.automationRules {
		if m.automationRules[i].ID == id {
			return &m.automationRules[i], nil
		}
	}
	return nil, oms.ErrNotFound
}

func (m *mockRepo) ListAutomationRules(_ context.Context, ontologyRID string) ([]oms.AutomationRule, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []oms.AutomationRule
	for _, r := range m.automationRules {
		if r.OntologyRID == ontologyRID {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockRepo) UpdateAutomationRule(_ context.Context, rule *oms.AutomationRule) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	for i := range m.automationRules {
		if m.automationRules[i].ID == rule.ID {
			m.automationRules[i] = *rule
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) DeleteAutomationRule(_ context.Context, id string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for i := range m.automationRules {
		if m.automationRules[i].ID == id {
			m.automationRules = append(m.automationRules[:i], m.automationRules[i+1:]...)
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) InsertExecution(_ context.Context, exec *oms.AutomationExecution) error {
	m.executions = append(m.executions, *exec)
	return nil
}

func (m *mockRepo) UpdateExecution(_ context.Context, exec *oms.AutomationExecution) error {
	for i := range m.executions {
		if m.executions[i].ID == exec.ID {
			m.executions[i] = *exec
			return nil
		}
	}
	return oms.ErrNotFound
}

func (m *mockRepo) ListExecutions(_ context.Context, ruleID string) ([]oms.AutomationExecution, error) {
	var result []oms.AutomationExecution
	for _, e := range m.executions {
		if e.RuleID == ruleID {
			result = append(result, e)
		}
	}
	return result, nil
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
func (n *noopRepo) UpdateExecution(_ context.Context, _ *oms.AutomationExecution) error { return nil }
func (n *noopRepo) ListExecutions(_ context.Context, _ string) ([]oms.AutomationExecution, error) {
	return nil, nil
}
