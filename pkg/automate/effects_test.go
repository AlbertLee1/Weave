package automate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

// mockActionApplier implements ActionApplier for testing.
type mockActionApplier struct {
	mu    sync.Mutex
	calls []actionApplierCall
	err   error // if non-nil, ApplyAction returns this error
}

type actionApplierCall struct {
	ontologyRID string
	actionType  string
	parameters  map[string]interface{}
}

func (m *mockActionApplier) ApplyAction(ctx context.Context, ontologyRID, actionType string, parameters map[string]interface{}) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, actionApplierCall{
		ontologyRID: ontologyRID,
		actionType:  actionType,
		parameters:  parameters,
	})
	return m.err
}

func (m *mockActionApplier) getCalls() []actionApplierCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]actionApplierCall, len(m.calls))
	copy(result, m.calls)
	return result
}

// --- ParseEffects tests ---

func TestParseEffects_ExecuteAction(t *testing.T) {
	raw := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"createEmployee","parameters":{"name":"Alice","department":"Engineering"}}]`)
	effects, err := ParseEffects(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}
	if effects[0].Type != "executeAction" {
		t.Fatalf("expected type 'executeAction', got %q", effects[0].Type)
	}
	if effects[0].ActionTypeApiName != "createEmployee" {
		t.Fatalf("expected actionTypeApiName 'createEmployee', got %q", effects[0].ActionTypeApiName)
	}
	if effects[0].Parameters["name"] != "Alice" {
		t.Fatalf("expected parameter name 'Alice', got %v", effects[0].Parameters["name"])
	}
}

func TestParseEffects_MultipleEffects(t *testing.T) {
	raw := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"a1"},{"type":"executeAction","actionTypeApiName":"a2"}]`)
	effects, err := ParseEffects(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(effects) != 2 {
		t.Fatalf("expected 2 effects, got %d", len(effects))
	}
}

func TestParseEffects_EmptyArray(t *testing.T) {
	raw := json.RawMessage(`[]`)
	effects, err := ParseEffects(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(effects) != 0 {
		t.Fatalf("expected 0 effects, got %d", len(effects))
	}
}

func TestParseEffects_Null(t *testing.T) {
	effects, err := ParseEffects(json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if effects != nil {
		t.Fatalf("expected nil effects, got %v", effects)
	}
}

func TestParseEffects_InvalidJSON(t *testing.T) {
	_, err := ParseEffects(json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// --- resolveTemplateString tests ---

func TestResolveTemplateString_PrimaryKey(t *testing.T) {
	data := &TriggerEventData{PrimaryKey: "emp-1"}
	result := resolveTemplateString("Object ${event.primaryKey} created", data)
	if result != "Object emp-1 created" {
		t.Fatalf("expected 'Object emp-1 created', got %q", result)
	}
}

func TestResolveTemplateString_EditType(t *testing.T) {
	data := &TriggerEventData{EditType: "CREATE"}
	result := resolveTemplateString("Action: ${event.editType}", data)
	if result != "Action: CREATE" {
		t.Fatalf("expected 'Action: CREATE', got %q", result)
	}
}

func TestResolveTemplateString_ObjectType(t *testing.T) {
	data := &TriggerEventData{ObjectType: "Employee"}
	result := resolveTemplateString("Type: ${event.objectType}", data)
	if result != "Type: Employee" {
		t.Fatalf("expected 'Type: Employee', got %q", result)
	}
}

func TestResolveTemplateString_Properties(t *testing.T) {
	data := &TriggerEventData{
		Properties: map[string]interface{}{
			"department": "Engineering",
			"name":       "Alice",
		},
	}
	result := resolveTemplateString("Dept: ${event.properties.department}", data)
	if result != "Dept: Engineering" {
		t.Fatalf("expected 'Dept: Engineering', got %q", result)
	}
}

func TestResolveTemplateString_NoTemplates(t *testing.T) {
	data := &TriggerEventData{PrimaryKey: "emp-1"}
	result := resolveTemplateString("Hello world", data)
	if result != "Hello world" {
		t.Fatalf("expected 'Hello world', got %q", result)
	}
}

func TestResolveTemplateString_MultipleVars(t *testing.T) {
	data := &TriggerEventData{
		PrimaryKey: "emp-1",
		EditType:   "CREATE",
		ObjectType: "Employee",
	}
	result := resolveTemplateString("${event.editType} ${event.objectType} ${event.primaryKey}", data)
	if result != "CREATE Employee emp-1" {
		t.Fatalf("expected 'CREATE Employee emp-1', got %q", result)
	}
}

func TestResolveTemplateString_NilData(t *testing.T) {
	result := resolveTemplateString("${event.primaryKey}", nil)
	if result != "${event.primaryKey}" {
		t.Fatalf("expected unchanged string, got %q", result)
	}
}

// --- resolveParameters tests ---

func TestResolveParameters(t *testing.T) {
	data := &TriggerEventData{
		PrimaryKey: "emp-1",
		EditType:   "CREATE",
		Properties: map[string]interface{}{
			"name": "Alice",
		},
	}
	params := map[string]interface{}{
		"employeeId": "${event.primaryKey}",
		"action":     "${event.editType}",
		"name":       "${event.properties.name}",
		"staticVal":  "hello",
		"number":     float64(42),
	}
	resolved := resolveParameters(params, data)

	if resolved["employeeId"] != "emp-1" {
		t.Fatalf("expected employeeId 'emp-1', got %v", resolved["employeeId"])
	}
	if resolved["action"] != "CREATE" {
		t.Fatalf("expected action 'CREATE', got %v", resolved["action"])
	}
	if resolved["name"] != "Alice" {
		t.Fatalf("expected name 'Alice', got %v", resolved["name"])
	}
	if resolved["staticVal"] != "hello" {
		t.Fatalf("expected staticVal 'hello', got %v", resolved["staticVal"])
	}
	if resolved["number"] != float64(42) {
		t.Fatalf("expected number 42, got %v", resolved["number"])
	}
}

func TestResolveParameters_NilParams(t *testing.T) {
	data := &TriggerEventData{PrimaryKey: "emp-1"}
	result := resolveParameters(nil, data)
	if result != nil {
		t.Fatalf("expected nil, got %v", result)
	}
}

// --- processEffects tests ---

func TestProcessEffects_ExecuteAction(t *testing.T) {
	applier := &mockActionApplier{}
	data := &TriggerEventData{PrimaryKey: "emp-1", EditType: "CREATE"}

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"createReport","parameters":{"pk":"${event.primaryKey}"}}]`)

	err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, applier)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := applier.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].ontologyRID != "ri.ontology.main.ontology.1" {
		t.Fatalf("expected ontologyRID, got %q", calls[0].ontologyRID)
	}
	if calls[0].actionType != "createReport" {
		t.Fatalf("expected actionType 'createReport', got %q", calls[0].actionType)
	}
	if calls[0].parameters["pk"] != "emp-1" {
		t.Fatalf("expected pk 'emp-1', got %v", calls[0].parameters["pk"])
	}
}

func TestProcessEffects_Failure(t *testing.T) {
	applier := &mockActionApplier{err: fmt.Errorf("action type not found")}
	data := &TriggerEventData{}

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"nonExistent","parameters":{}}]`)

	err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, applier)
	if err == nil {
		t.Fatal("expected error from failing action")
	}
	if !contains(err.Error(), "action type not found") {
		t.Fatalf("expected error to contain 'action type not found', got %q", err.Error())
	}
}

func TestProcessEffects_NilApplier(t *testing.T) {
	data := &TriggerEventData{}
	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"skip","parameters":{}}]`)

	// Should not panic or error when applier is nil
	err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessEffects_UnknownEffectType(t *testing.T) {
	applier := &mockActionApplier{}
	data := &TriggerEventData{}
	effects := json.RawMessage(`[{"type":"unknownFuture","parameters":{}}]`)

	// Unknown types should be silently skipped
	err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, applier)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(applier.getCalls()) != 0 {
		t.Fatal("expected no action calls for unknown effect type")
	}
}

func TestProcessEffects_EmptyEffects(t *testing.T) {
	applier := &mockActionApplier{}
	data := &TriggerEventData{}

	err := processEffects(context.Background(), json.RawMessage(`[]`), "ri.ontology.main.ontology.1", data, applier)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessEffects_MultipleActions_StopsOnFirstError(t *testing.T) {
	applier := &mockActionApplier{err: fmt.Errorf("fail")}
	data := &TriggerEventData{}

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"a1","parameters":{}},{"type":"executeAction","actionTypeApiName":"a2","parameters":{}}]`)

	err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, applier)
	if err == nil {
		t.Fatal("expected error")
	}
	// Should stop after first failure — only 1 call
	if len(applier.getCalls()) != 1 {
		t.Fatalf("expected 1 call (stop on first error), got %d", len(applier.getCalls()))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
