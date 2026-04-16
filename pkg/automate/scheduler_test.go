package automate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// mockRuleLoader implements RuleLoader for testing.
type mockRuleLoader struct {
	mu    sync.Mutex
	rules []oms.AutomationRule
}

func (m *mockRuleLoader) ListActiveScheduleRules(ctx context.Context) ([]oms.AutomationRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []oms.AutomationRule
	for _, r := range m.rules {
		if r.Status == "active" && r.TriggerType == "schedule" {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockRuleLoader) addRule(rule oms.AutomationRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules, rule)
}

func (m *mockRuleLoader) updateStatus(id, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.rules {
		if m.rules[i].ID == id {
			m.rules[i].Status = status
		}
	}
}

// mockExecutionRecorder implements ExecutionRecorder for testing.
type mockExecutionRecorder struct {
	mu         sync.Mutex
	executions []oms.AutomationExecution
}

func (m *mockExecutionRecorder) InsertExecution(ctx context.Context, exec *oms.AutomationExecution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executions = append(m.executions, *exec)
	return nil
}

func (m *mockExecutionRecorder) getExecutions() []oms.AutomationExecution {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]oms.AutomationExecution, len(m.executions))
	copy(result, m.executions)
	return result
}

func makeTriggerConfig(cron string) json.RawMessage {
	b, _ := json.Marshal(ScheduleTriggerConfig{Cron: cron})
	return b
}

func TestParseTriggerConfig(t *testing.T) {
	t.Run("valid cron expression", func(t *testing.T) {
		cfg := makeTriggerConfig("*/5 * * * *")
		tc, err := ParseScheduleTriggerConfig(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tc.Cron != "*/5 * * * *" {
			t.Fatalf("expected cron '*/5 * * * *', got %q", tc.Cron)
		}
	})

	t.Run("empty cron", func(t *testing.T) {
		cfg := makeTriggerConfig("")
		_, err := ParseScheduleTriggerConfig(cfg)
		if err == nil {
			t.Fatal("expected error for empty cron")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := ParseScheduleTriggerConfig(json.RawMessage(`{invalid`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestSchedulerNew(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	s := New(loader, recorder)
	if s == nil {
		t.Fatal("expected non-nil scheduler")
	}
	if s.loader != loader {
		t.Fatal("expected loader to be set")
	}
	if s.recorder != recorder {
		t.Fatal("expected recorder to be set")
	}
}

func TestSchedulerStartStop(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	s := New(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error starting scheduler: %v", err)
	}

	// Should be running
	s.Stop()
	// Stop should be idempotent
	s.Stop()
}

func TestSchedulerLoadsActiveRulesOnStart(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	// Add an active schedule rule
	loader.addRule(oms.AutomationRule{
		ID:            "rule-1",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Hourly Report",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("0 * * * *"),
	})

	// Add a paused schedule rule (should not be loaded)
	loader.addRule(oms.AutomationRule{
		ID:            "rule-2",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Paused Rule",
		Status:        "paused",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("0 * * * *"),
	})

	// Add a dataChange rule (should not be loaded)
	loader.addRule(oms.AutomationRule{
		ID:            "rule-3",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Data Change Rule",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: json.RawMessage(`{}`),
	})

	s := New(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error starting scheduler: %v", err)
	}
	defer s.Stop()

	// Only the active schedule rule should be loaded
	s.mu.Lock()
	count := len(s.entries)
	s.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 scheduled entry, got %d", count)
	}
}

func TestSchedulerCronFiresExecution(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	// Use every-second cron for testing (robfig/cron supports @every)
	loader.addRule(oms.AutomationRule{
		ID:            "rule-fire",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Fast Rule",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("@every 1s"),
		Effects:       json.RawMessage(`[{"type":"log","message":"hello"}]`),
	})

	s := New(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Stop()

	// Wait for at least one execution
	deadline := time.After(5 * time.Second)
	for {
		execs := recorder.getExecutions()
		if len(execs) > 0 {
			exec := execs[0]
			if exec.RuleID != "rule-fire" {
				t.Fatalf("expected ruleId 'rule-fire', got %q", exec.RuleID)
			}
			if exec.Status != "success" {
				t.Fatalf("expected status 'success', got %q", exec.Status)
			}
			if exec.CompletedAt == nil {
				t.Fatal("expected completedAt to be set")
			}
			if exec.ID == "" {
				t.Fatal("expected execution ID to be set")
			}
			// Check trigger event
			var triggerEvent map[string]interface{}
			if err := json.Unmarshal(exec.TriggerEvent, &triggerEvent); err != nil {
				t.Fatalf("failed to unmarshal trigger event: %v", err)
			}
			if triggerEvent["type"] != "schedule" {
				t.Fatalf("expected trigger type 'schedule', got %v", triggerEvent["type"])
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for execution")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestSchedulerAddRule(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	s := New(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Stop()

	// Dynamically add a rule
	rule := oms.AutomationRule{
		ID:            "rule-dynamic",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Dynamic Rule",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("@every 1s"),
		Effects:       json.RawMessage(`[]`),
	}

	err = s.AddRule(rule)
	if err != nil {
		t.Fatalf("unexpected error adding rule: %v", err)
	}

	s.mu.Lock()
	count := len(s.entries)
	s.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 entry, got %d", count)
	}

	// Wait for execution
	deadline := time.After(5 * time.Second)
	for {
		execs := recorder.getExecutions()
		if len(execs) > 0 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for dynamically added rule to fire")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestSchedulerRemoveRule(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	s := New(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Stop()

	// Add then remove
	rule := oms.AutomationRule{
		ID:            "rule-remove",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "To Remove",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("0 * * * *"),
	}

	err = s.AddRule(rule)
	if err != nil {
		t.Fatalf("unexpected error adding rule: %v", err)
	}

	s.RemoveRule("rule-remove")

	s.mu.Lock()
	count := len(s.entries)
	s.mu.Unlock()

	if count != 0 {
		t.Fatalf("expected 0 entries after removal, got %d", count)
	}
}

func TestSchedulerRemoveNonExistentRule(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	s := New(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Stop()

	// Should not panic
	s.RemoveRule("nonexistent")
}

func TestSchedulerPauseResume(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	s := New(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Stop()

	// Add a fast-firing rule
	rule := oms.AutomationRule{
		ID:            "rule-pause",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Pausable",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("@every 1s"),
		Effects:       json.RawMessage(`[]`),
	}

	err = s.AddRule(rule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pause should remove from scheduler
	s.PauseRule("rule-pause")

	s.mu.Lock()
	count := len(s.entries)
	s.mu.Unlock()

	if count != 0 {
		t.Fatalf("expected 0 entries after pause, got %d", count)
	}

	// Resume should re-add
	err = s.ResumeRule(rule)
	if err != nil {
		t.Fatalf("unexpected error resuming: %v", err)
	}

	s.mu.Lock()
	count = len(s.entries)
	s.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 entry after resume, got %d", count)
	}
}

func TestScheduler_ExecuteActionEffect(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}
	applier := &mockActionApplier{}

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"generateReport","parameters":{"source":"scheduled"}}]`)

	loader.addRule(oms.AutomationRule{
		ID:            "rule-sched-action",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Scheduled Action",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("@every 1s"),
		Effects:       effects,
	})

	s := New(loader, recorder)
	s.SetActionApplier(applier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Stop()

	// Wait for cron to fire
	deadline := time.After(5 * time.Second)
	for {
		calls := applier.getCalls()
		if len(calls) > 0 {
			if calls[0].ontologyRID != "ri.ontology.main.ontology.1" {
				t.Fatalf("expected ontologyRID, got %q", calls[0].ontologyRID)
			}
			if calls[0].actionType != "generateReport" {
				t.Fatalf("expected actionType 'generateReport', got %q", calls[0].actionType)
			}
			if calls[0].parameters["source"] != "scheduled" {
				t.Fatalf("expected source 'scheduled', got %v", calls[0].parameters["source"])
			}

			// Verify execution recorded as success
			execs := recorder.getExecutions()
			if len(execs) == 0 {
				t.Fatal("expected at least 1 execution")
			}
			if execs[0].Status != "success" {
				t.Fatalf("expected status 'success', got %q", execs[0].Status)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for scheduled action effect")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestScheduler_ExecuteActionEffect_Failure(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}
	applier := &mockActionApplier{err: fmt.Errorf("action failed")}

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"badAction","parameters":{}}]`)

	loader.addRule(oms.AutomationRule{
		ID:            "rule-sched-fail",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Failing Scheduled Action",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("@every 1s"),
		Effects:       effects,
	})

	s := New(loader, recorder)
	s.SetActionApplier(applier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.Start(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Stop()

	// Wait for cron to fire
	deadline := time.After(5 * time.Second)
	for {
		execs := recorder.getExecutions()
		if len(execs) > 0 {
			if execs[0].Status != "error" {
				t.Fatalf("expected status 'error', got %q", execs[0].Status)
			}
			if execs[0].Error == "" {
				t.Fatal("expected non-empty error message")
			}
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for scheduled execution")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func TestSchedulerInvalidCronExpression(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	s := New(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Stop()

	rule := oms.AutomationRule{
		ID:            "rule-bad-cron",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Bad Cron",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("not a valid cron"),
	}

	err = s.AddRule(rule)
	if err == nil {
		t.Fatal("expected error for invalid cron expression")
	}
}

func TestSchedulerInvalidTriggerConfig(t *testing.T) {
	loader := &mockRuleLoader{}
	recorder := &mockExecutionRecorder{}

	s := New(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := s.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer s.Stop()

	rule := oms.AutomationRule{
		ID:            "rule-bad-config",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Bad Config",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: json.RawMessage(`{invalid`),
	}

	err = s.AddRule(rule)
	if err == nil {
		t.Fatal("expected error for invalid trigger config JSON")
	}
}
