package automate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// mockDataChangeRuleLoader implements DataChangeRuleLoader for testing.
type mockDataChangeRuleLoader struct {
	mu    sync.Mutex
	rules []oms.AutomationRule
}

func (m *mockDataChangeRuleLoader) ListActiveDataChangeRules(ctx context.Context) ([]oms.AutomationRule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []oms.AutomationRule
	for _, r := range m.rules {
		if r.Status == "active" && r.TriggerType == "dataChange" {
			result = append(result, r)
		}
	}
	return result, nil
}

func (m *mockDataChangeRuleLoader) addRule(rule oms.AutomationRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = append(m.rules, rule)
}

// mockPropertyFetcher implements ObjectPropertyFetcher for testing.
type mockPropertyFetcher struct {
	mu    sync.Mutex
	store map[string]map[string]interface{} // "objectType:primaryKey" → properties
}

func newMockPropertyFetcher() *mockPropertyFetcher {
	return &mockPropertyFetcher{
		store: make(map[string]map[string]interface{}),
	}
}

func (m *mockPropertyFetcher) FetchProperties(ctx context.Context, objectType, primaryKey string) (map[string]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := objectType + ":" + primaryKey
	if props, ok := m.store[key]; ok {
		return props, nil
	}
	return nil, nil
}

func (m *mockPropertyFetcher) setProperties(objectType, primaryKey string, props map[string]interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.store[objectType+":"+primaryKey] = props
}

func makeDataChangeTriggerConfig(objectType string, editTypes []string, whereClause json.RawMessage, debounceMs int) json.RawMessage {
	cfg := DataChangeTriggerConfig{
		ObjectType: objectType,
		EditTypes:  editTypes,
	}
	if whereClause != nil {
		cfg.Where = whereClause
	}
	if debounceMs > 0 {
		cfg.DebounceMs = debounceMs
	}
	b, _ := json.Marshal(cfg)
	return b
}

func TestParseDataChangeTriggerConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		raw := makeDataChangeTriggerConfig("Employee", []string{"CREATE", "MODIFY"}, nil, 0)
		cfg, err := ParseDataChangeTriggerConfig(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.ObjectType != "Employee" {
			t.Fatalf("expected objectType 'Employee', got %q", cfg.ObjectType)
		}
		if len(cfg.EditTypes) != 2 {
			t.Fatalf("expected 2 editTypes, got %d", len(cfg.EditTypes))
		}
		if cfg.EditTypes[0] != "CREATE" || cfg.EditTypes[1] != "MODIFY" {
			t.Fatalf("unexpected editTypes: %v", cfg.EditTypes)
		}
	})

	t.Run("with where clause", func(t *testing.T) {
		where := json.RawMessage(`{"type":"eq","field":"department","value":"\"Engineering\""}`)
		raw := makeDataChangeTriggerConfig("Employee", []string{"MODIFY"}, where, 0)
		cfg, err := ParseDataChangeTriggerConfig(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Where == nil {
			t.Fatal("expected where clause to be present")
		}
	})

	t.Run("with debounce", func(t *testing.T) {
		raw := makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 500)
		cfg, err := ParseDataChangeTriggerConfig(raw)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.DebounceMs != 500 {
			t.Fatalf("expected debounceMs 500, got %d", cfg.DebounceMs)
		}
	})

	t.Run("empty objectType", func(t *testing.T) {
		raw := makeDataChangeTriggerConfig("", []string{"CREATE"}, nil, 0)
		_, err := ParseDataChangeTriggerConfig(raw)
		if err == nil {
			t.Fatal("expected error for empty objectType")
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := ParseDataChangeTriggerConfig(json.RawMessage(`{invalid`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestWatcherNew(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	w := NewWatcher(loader, recorder)
	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
}

func TestWatcherStartStop(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	w := NewWatcher(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := w.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error starting watcher: %v", err)
	}

	w.Stop()
	// Stop should be idempotent
	w.Stop()
}

func TestWatcherLoadsActiveRulesOnStart(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	// Active dataChange rule — should be loaded
	loader.addRule(oms.AutomationRule{
		ID:            "rule-1",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Employee Change",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE", "MODIFY"}, nil, 0),
	})

	// Paused dataChange rule — should NOT be loaded
	loader.addRule(oms.AutomationRule{
		ID:            "rule-2",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Paused Rule",
		Status:        "paused",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
	})

	// Active schedule rule — should NOT be loaded
	loader.addRule(oms.AutomationRule{
		ID:            "rule-3",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Schedule Rule",
		Status:        "active",
		TriggerType:   "schedule",
		TriggerConfig: makeTriggerConfig("0 * * * *"),
	})

	w := NewWatcher(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := w.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer w.Stop()

	w.mu.Lock()
	count := len(w.rules)
	w.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 active rule, got %d", count)
	}
}

func TestWatcherHandleChangeEvent_ObjectTypeMatch(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	loader.addRule(oms.AutomationRule{
		ID:            "rule-emp",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Employee Create",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       json.RawMessage(`[{"type":"log"}]`),
	})

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Matching event: Employee CREATE
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	// Wait briefly for execution
	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
	if execs[0].RuleID != "rule-emp" {
		t.Fatalf("expected ruleID 'rule-emp', got %q", execs[0].RuleID)
	}
	if execs[0].Status != "success" {
		t.Fatalf("expected status 'success', got %q", execs[0].Status)
	}

	// Verify trigger event metadata
	var triggerEvent map[string]interface{}
	if err := json.Unmarshal(execs[0].TriggerEvent, &triggerEvent); err != nil {
		t.Fatalf("failed to unmarshal trigger event: %v", err)
	}
	if triggerEvent["type"] != "dataChange" {
		t.Fatalf("expected trigger type 'dataChange', got %v", triggerEvent["type"])
	}
	if triggerEvent["objectType"] != "Employee" {
		t.Fatalf("expected objectType 'Employee', got %v", triggerEvent["objectType"])
	}
	if triggerEvent["primaryKey"] != "emp-1" {
		t.Fatalf("expected primaryKey 'emp-1', got %v", triggerEvent["primaryKey"])
	}
	if triggerEvent["editType"] != "CREATE" {
		t.Fatalf("expected editType 'CREATE', got %v", triggerEvent["editType"])
	}
}

func TestWatcherHandleChangeEvent_NoMatchObjectType(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	loader.addRule(oms.AutomationRule{
		ID:            "rule-emp",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Employee Create",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
	})

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Non-matching: Department CREATE (rule is for Employee)
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Department",
		PrimaryKey: "dept-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 0 {
		t.Fatalf("expected 0 executions, got %d", len(execs))
	}
}

func TestWatcherHandleChangeEvent_NoMatchEditType(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	loader.addRule(oms.AutomationRule{
		ID:            "rule-emp",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Employee Create Only",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
	})

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Non-matching: Employee MODIFY (rule only allows CREATE)
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeModify,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 0 {
		t.Fatalf("expected 0 executions, got %d", len(execs))
	}
}

func TestWatcherHandleChangeEvent_WhereClauseMatch(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}
	fetcher := newMockPropertyFetcher()

	// Rule with where clause: department == "Engineering"
	whereClause := json.RawMessage(`{"type":"eq","field":"department","value":"Engineering"}`)
	loader.addRule(oms.AutomationRule{
		ID:            "rule-eng",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Engineering Employee Change",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE", "MODIFY"}, whereClause, 0),
		Effects:       json.RawMessage(`[]`),
	})

	// Set up mock data: emp-1 is in Engineering
	fetcher.setProperties("Employee", "emp-1", map[string]interface{}{
		"department": "Engineering",
		"name":       "Alice",
	})

	w := NewWatcher(loader, recorder)
	w.SetPropertyFetcher(fetcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Matching event: Employee CREATE with matching where
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
}

func TestWatcherHandleChangeEvent_WhereClauseNoMatch(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}
	fetcher := newMockPropertyFetcher()

	// Rule with where clause: department == "Engineering"
	whereClause := json.RawMessage(`{"type":"eq","field":"department","value":"Engineering"}`)
	loader.addRule(oms.AutomationRule{
		ID:            "rule-eng",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Engineering Employee Change",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE", "MODIFY"}, whereClause, 0),
	})

	// Set up mock data: emp-2 is in Marketing (no match)
	fetcher.setProperties("Employee", "emp-2", map[string]interface{}{
		"department": "Marketing",
		"name":       "Bob",
	})

	w := NewWatcher(loader, recorder)
	w.SetPropertyFetcher(fetcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Non-matching event: department is Marketing, not Engineering
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-2",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 0 {
		t.Fatalf("expected 0 executions, got %d", len(execs))
	}
}

func TestWatcherHandleChangeEvent_NoWhereClauseMatchesAll(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	// Rule WITHOUT where clause — should match all Employee CREATEs
	loader.addRule(oms.AutomationRule{
		ID:            "rule-all",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "All Employee Creates",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       json.RawMessage(`[]`),
	})

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-any",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
}

func TestWatcherHandleChangeEvent_MultipleRules(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	// Rule 1: all Employee CREATEs
	loader.addRule(oms.AutomationRule{
		ID:            "rule-1",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Employee Create",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       json.RawMessage(`[]`),
	})

	// Rule 2: all Employee CREATE + MODIFY
	loader.addRule(oms.AutomationRule{
		ID:            "rule-2",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Employee Create or Modify",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE", "MODIFY"}, nil, 0),
		Effects:       json.RawMessage(`[]`),
	})

	// Rule 3: Department DELETE
	loader.addRule(oms.AutomationRule{
		ID:            "rule-3",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Department Delete",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Department", []string{"DELETE"}, nil, 0),
		Effects:       json.RawMessage(`[]`),
	})

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Employee CREATE should match rule-1 and rule-2, not rule-3
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 2 {
		t.Fatalf("expected 2 executions (2 matching rules), got %d", len(execs))
	}

	// Verify both rules were triggered
	ruleIDs := map[string]bool{}
	for _, e := range execs {
		ruleIDs[e.RuleID] = true
	}
	if !ruleIDs["rule-1"] || !ruleIDs["rule-2"] {
		t.Fatalf("expected rule-1 and rule-2, got: %v", ruleIDs)
	}
}

func TestWatcherAddRule(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Dynamically add a rule
	rule := oms.AutomationRule{
		ID:            "rule-dynamic",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Dynamic Rule",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       json.RawMessage(`[]`),
	}

	err := w.AddRule(rule)
	if err != nil {
		t.Fatalf("unexpected error adding rule: %v", err)
	}

	w.mu.Lock()
	count := len(w.rules)
	w.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 rule, got %d", count)
	}

	// Verify rule fires on matching event
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
}

func TestWatcherRemoveRule(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	loader.addRule(oms.AutomationRule{
		ID:            "rule-remove",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "To Remove",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
	})

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	w.RemoveRule("rule-remove")

	w.mu.Lock()
	count := len(w.rules)
	w.mu.Unlock()

	if count != 0 {
		t.Fatalf("expected 0 rules after removal, got %d", count)
	}

	// Verify event no longer matches
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 0 {
		t.Fatalf("expected 0 executions after rule removal, got %d", len(execs))
	}
}

func TestWatcherPauseResume(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	rule := oms.AutomationRule{
		ID:            "rule-pause",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Pausable",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       json.RawMessage(`[]`),
	}
	loader.addRule(rule)

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Pause should remove from rules
	w.PauseRule("rule-pause")

	w.mu.Lock()
	count := len(w.rules)
	w.mu.Unlock()

	if count != 0 {
		t.Fatalf("expected 0 rules after pause, got %d", count)
	}

	// Resume should re-add
	err := w.ResumeRule(rule)
	if err != nil {
		t.Fatalf("unexpected error resuming: %v", err)
	}

	w.mu.Lock()
	count = len(w.rules)
	w.mu.Unlock()

	if count != 1 {
		t.Fatalf("expected 1 rule after resume, got %d", count)
	}
}

func TestWatcherDebounce(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	// Rule with 200ms debounce
	loader.addRule(oms.AutomationRule{
		ID:            "rule-debounce",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Debounced Rule",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE", "MODIFY"}, nil, 200),
		Effects:       json.RawMessage(`[]`),
	})

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Fire 3 rapid events within debounce window
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})
	time.Sleep(50 * time.Millisecond)

	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-2",
		EditType:   funnel.EditTypeModify,
	})
	time.Sleep(50 * time.Millisecond)

	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-3",
		EditType:   funnel.EditTypeModify,
	})

	// Before debounce expires: should have 0 executions
	execs := recorder.getExecutions()
	if len(execs) != 0 {
		t.Fatalf("expected 0 executions before debounce expires, got %d", len(execs))
	}

	// Wait for debounce to expire (200ms from last event)
	time.Sleep(350 * time.Millisecond)

	execs = recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected exactly 1 execution after debounce, got %d", len(execs))
	}

	// Verify the execution records the LAST event
	var triggerEvent map[string]interface{}
	if err := json.Unmarshal(execs[0].TriggerEvent, &triggerEvent); err != nil {
		t.Fatalf("failed to unmarshal trigger event: %v", err)
	}
	if triggerEvent["primaryKey"] != "emp-3" {
		t.Fatalf("expected last event primaryKey 'emp-3', got %v", triggerEvent["primaryKey"])
	}
}

func TestWatcherDebounce_ZeroMeansImmediate(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	// Rule with no debounce (default 0)
	loader.addRule(oms.AutomationRule{
		ID:            "rule-no-debounce",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Immediate Rule",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       json.RawMessage(`[]`),
	})

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Fire 3 events — each should execute immediately
	for i := 0; i < 3; i++ {
		w.HandleChangeEvent(funnel.ChangeEvent{
			ObjectType: "Employee",
			PrimaryKey: "emp-" + string(rune('1'+i)),
			EditType:   funnel.EditTypeCreate,
		})
	}

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 3 {
		t.Fatalf("expected 3 immediate executions, got %d", len(execs))
	}
}

func TestWatcherHandleChangeEvent_WhereClauseNoFetcherSkipsWhere(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	// Rule WITH where clause but NO property fetcher
	whereClause := json.RawMessage(`{"type":"eq","field":"department","value":"Engineering"}`)
	loader.addRule(oms.AutomationRule{
		ID:            "rule-where-no-fetcher",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Where without fetcher",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, whereClause, 0),
		Effects:       json.RawMessage(`[]`),
	})

	// No property fetcher set — where clause should be skipped (treated as match)
	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution (where skipped without fetcher), got %d", len(execs))
	}
}

func TestWatcherInvalidTriggerConfig(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Try adding rule with invalid trigger config
	rule := oms.AutomationRule{
		ID:            "rule-bad-config",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Bad Config",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: json.RawMessage(`{invalid`),
	}

	err := w.AddRule(rule)
	if err == nil {
		t.Fatal("expected error for invalid trigger config")
	}
}

func TestWatcherRemoveNonExistentRule(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// Should not panic
	w.RemoveRule("nonexistent")
}

func TestWatcher_ExecuteActionEffect(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}
	applier := &mockActionApplier{}

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"createReport","parameters":{"employeeId":"${event.primaryKey}","action":"${event.editType}"}}]`)

	loader.addRule(oms.AutomationRule{
		ID:            "rule-action",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Action Effect Rule",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       effects,
	})

	w := NewWatcher(loader, recorder)
	w.SetActionApplier(applier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	// Verify action was applied with resolved templates
	calls := applier.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 action call, got %d", len(calls))
	}
	if calls[0].ontologyRID != "ri.ontology.main.ontology.1" {
		t.Fatalf("expected ontologyRID 'ri.ontology.main.ontology.1', got %q", calls[0].ontologyRID)
	}
	if calls[0].actionType != "createReport" {
		t.Fatalf("expected actionType 'createReport', got %q", calls[0].actionType)
	}
	if calls[0].parameters["employeeId"] != "emp-1" {
		t.Fatalf("expected employeeId 'emp-1', got %v", calls[0].parameters["employeeId"])
	}
	if calls[0].parameters["action"] != "CREATE" {
		t.Fatalf("expected action 'CREATE', got %v", calls[0].parameters["action"])
	}

	// Verify execution recorded as success
	execs := recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
	if execs[0].Status != "success" {
		t.Fatalf("expected status 'success', got %q", execs[0].Status)
	}
}

func TestWatcher_ExecuteActionEffect_TemplateWithProperties(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}
	applier := &mockActionApplier{}
	fetcher := newMockPropertyFetcher()

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"notifyDept","parameters":{"dept":"${event.properties.department}","pk":"${event.primaryKey}"}}]`)

	loader.addRule(oms.AutomationRule{
		ID:            "rule-props",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Props Rule",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       effects,
	})

	fetcher.setProperties("Employee", "emp-1", map[string]interface{}{
		"department": "Engineering",
	})

	w := NewWatcher(loader, recorder)
	w.SetActionApplier(applier)
	w.SetPropertyFetcher(fetcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	calls := applier.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].parameters["dept"] != "Engineering" {
		t.Fatalf("expected dept 'Engineering', got %v", calls[0].parameters["dept"])
	}
	if calls[0].parameters["pk"] != "emp-1" {
		t.Fatalf("expected pk 'emp-1', got %v", calls[0].parameters["pk"])
	}
}

func TestWatcher_ExecuteActionEffect_Failure(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}
	applier := &mockActionApplier{err: fmt.Errorf("action type not found")}

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"nonExistent","parameters":{}}]`)

	loader.addRule(oms.AutomationRule{
		ID:            "rule-fail",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Failing Rule",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       effects,
	})

	w := NewWatcher(loader, recorder)
	w.SetActionApplier(applier)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
	if execs[0].Status != "error" {
		t.Fatalf("expected status 'error', got %q", execs[0].Status)
	}
	if execs[0].Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestWatcher_ExecuteActionEffect_NoApplier(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"skip","parameters":{}}]`)

	loader.addRule(oms.AutomationRule{
		ID:            "rule-no-applier",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "No Applier Rule",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       effects,
	})

	// No SetActionApplier — should degrade gracefully
	w := NewWatcher(loader, recorder)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	// Should still record execution as success (effect skipped gracefully)
	execs := recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
	if execs[0].Status != "success" {
		t.Fatalf("expected status 'success', got %q", execs[0].Status)
	}
}

func TestWatcher_ExecuteFunctionEffect(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}
	dispatcher := &mockFunctionDispatcher{result: map[string]interface{}{"score": float64(95)}}

	effects := json.RawMessage(`[{"type":"executeFunction","functionRid":"ri.function.main.function.score","parameters":{"pk":"${event.primaryKey}"}}]`)

	loader.addRule(oms.AutomationRule{
		ID:            "rule-func-dc",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Function on DataChange",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE"}, nil, 0),
		Effects:       effects,
	})

	w := NewWatcher(loader, recorder)
	w.SetFunctionDispatcher(dispatcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeCreate,
	})

	time.Sleep(50 * time.Millisecond)

	calls := dispatcher.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 function call, got %d", len(calls))
	}
	if calls[0].functionRid != "ri.function.main.function.score" {
		t.Fatalf("expected functionRid, got %q", calls[0].functionRid)
	}
	if calls[0].parameters["pk"] != "emp-1" {
		t.Fatalf("expected pk 'emp-1', got %v", calls[0].parameters["pk"])
	}

	// Verify execution with result stored
	execs := recorder.getExecutions()
	if len(execs) != 1 {
		t.Fatalf("expected 1 execution, got %d", len(execs))
	}
	if execs[0].Status != "success" {
		t.Fatalf("expected status 'success', got %q", execs[0].Status)
	}
	if execs[0].Result == nil {
		t.Fatal("expected result to be stored in execution")
	}
}

func TestWatcherLinkEditTypesIgnored(t *testing.T) {
	loader := &mockDataChangeRuleLoader{}
	recorder := &mockExecutionRecorder{}

	// Rule listening for CREATE only
	loader.addRule(oms.AutomationRule{
		ID:            "rule-obj",
		OntologyRID:   "ri.ontology.main.ontology.1",
		Name:          "Object Changes Only",
		Status:        "active",
		TriggerType:   "dataChange",
		TriggerConfig: makeDataChangeTriggerConfig("Employee", []string{"CREATE", "MODIFY", "DELETE"}, nil, 0),
		Effects:       json.RawMessage(`[]`),
	})

	w := NewWatcher(loader, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	// LINK_CREATE should NOT match (not in editTypes list)
	w.HandleChangeEvent(funnel.ChangeEvent{
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		EditType:   funnel.EditTypeLinkCreate,
	})

	time.Sleep(50 * time.Millisecond)

	execs := recorder.getExecutions()
	if len(execs) != 0 {
		t.Fatalf("expected 0 executions for LINK_CREATE, got %d", len(execs))
	}
}
