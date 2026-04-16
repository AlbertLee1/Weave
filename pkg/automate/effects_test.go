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

	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, applier, nil, nil)
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

	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, applier, nil, nil)
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
	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessEffects_UnknownEffectType(t *testing.T) {
	applier := &mockActionApplier{}
	data := &TriggerEventData{}
	effects := json.RawMessage(`[{"type":"unknownFuture","parameters":{}}]`)

	// Unknown types should be silently skipped
	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, applier, nil, nil)
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

	_, err := processEffects(context.Background(), json.RawMessage(`[]`), "ri.ontology.main.ontology.1", data, applier, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessEffects_MultipleActions_StopsOnFirstError(t *testing.T) {
	applier := &mockActionApplier{err: fmt.Errorf("fail")}
	data := &TriggerEventData{}

	effects := json.RawMessage(`[{"type":"executeAction","actionTypeApiName":"a1","parameters":{}},{"type":"executeAction","actionTypeApiName":"a2","parameters":{}}]`)

	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, applier, nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	// Should stop after first failure — only 1 call
	if len(applier.getCalls()) != 1 {
		t.Fatalf("expected 1 call (stop on first error), got %d", len(applier.getCalls()))
	}
}

// mockFunctionDispatcher implements AutomateFunctionDispatcher for testing.
type mockFunctionDispatcher struct {
	mu     sync.Mutex
	calls  []functionDispatcherCall
	result interface{} // returned by DispatchFunction
	err    error       // if non-nil, DispatchFunction returns this error
}

type functionDispatcherCall struct {
	functionRid string
	parameters  map[string]interface{}
}

func (m *mockFunctionDispatcher) DispatchFunction(ctx context.Context, functionRid string, parameters map[string]interface{}) (interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, functionDispatcherCall{
		functionRid: functionRid,
		parameters:  parameters,
	})
	return m.result, m.err
}

func (m *mockFunctionDispatcher) getCalls() []functionDispatcherCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]functionDispatcherCall, len(m.calls))
	copy(result, m.calls)
	return result
}

// --- ParseEffects tests for executeFunction ---

func TestParseEffects_ExecuteFunction(t *testing.T) {
	raw := json.RawMessage(`[{"type":"executeFunction","functionRid":"ri.function.main.function.calc","parameters":{"x":10,"y":20}}]`)
	effects, err := ParseEffects(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}
	if effects[0].Type != "executeFunction" {
		t.Fatalf("expected type 'executeFunction', got %q", effects[0].Type)
	}
	if effects[0].FunctionRid != "ri.function.main.function.calc" {
		t.Fatalf("expected functionRid 'ri.function.main.function.calc', got %q", effects[0].FunctionRid)
	}
	if effects[0].Parameters["x"] != float64(10) {
		t.Fatalf("expected parameter x=10, got %v", effects[0].Parameters["x"])
	}
}

// --- processEffects tests for executeFunction ---

func TestProcessEffects_ExecuteFunction(t *testing.T) {
	dispatcher := &mockFunctionDispatcher{result: map[string]interface{}{"total": float64(42)}}
	data := &TriggerEventData{PrimaryKey: "obj-1"}

	effects := json.RawMessage(`[{"type":"executeFunction","functionRid":"ri.function.main.function.calc","parameters":{"pk":"${event.primaryKey}"}}]`)

	results, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, dispatcher, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := dispatcher.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].functionRid != "ri.function.main.function.calc" {
		t.Fatalf("expected functionRid, got %q", calls[0].functionRid)
	}
	if calls[0].parameters["pk"] != "obj-1" {
		t.Fatalf("expected pk 'obj-1', got %v", calls[0].parameters["pk"])
	}

	// Check result captured
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	resultMap, ok := results[0].Result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map result, got %T", results[0].Result)
	}
	if resultMap["total"] != float64(42) {
		t.Fatalf("expected total=42, got %v", resultMap["total"])
	}
}

func TestProcessEffects_ExecuteFunction_Failure(t *testing.T) {
	dispatcher := &mockFunctionDispatcher{err: fmt.Errorf("function not found")}
	data := &TriggerEventData{}

	effects := json.RawMessage(`[{"type":"executeFunction","functionRid":"ri.function.main.function.bad","parameters":{}}]`)

	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, dispatcher, nil)
	if err == nil {
		t.Fatal("expected error from failing function")
	}
	if !contains(err.Error(), "function not found") {
		t.Fatalf("expected error to contain 'function not found', got %q", err.Error())
	}
}

func TestProcessEffects_ExecuteFunction_NilDispatcher(t *testing.T) {
	data := &TriggerEventData{}
	effects := json.RawMessage(`[{"type":"executeFunction","functionRid":"ri.function.main.function.skip","parameters":{}}]`)

	// Should not panic or error when dispatcher is nil
	results, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result (empty), got %d", len(results))
	}
}

// --- Chain support tests ---

func TestProcessEffects_ChainReference(t *testing.T) {
	// First effect: executeFunction returns result
	// Second effect: executeAction references ${effects[0].result}
	dispatcher := &mockFunctionDispatcher{result: "computed-value-123"}
	applier := &mockActionApplier{}
	data := &TriggerEventData{}

	effects := json.RawMessage(`[
		{"type":"executeFunction","functionRid":"ri.function.main.function.calc","parameters":{"input":"test"}},
		{"type":"executeAction","actionTypeApiName":"useResult","parameters":{"value":"${effects[0].result}"}}
	]`)

	results, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, applier, dispatcher, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify chain resolution
	calls := applier.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 action call, got %d", len(calls))
	}
	if calls[0].parameters["value"] != "computed-value-123" {
		t.Fatalf("expected chain-resolved value 'computed-value-123', got %v", calls[0].parameters["value"])
	}

	// Verify both results tracked
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Result != "computed-value-123" {
		t.Fatalf("expected first result 'computed-value-123', got %v", results[0].Result)
	}
}

func TestResolveTemplateStringWithChain(t *testing.T) {
	results := []EffectResult{
		{Result: "value-from-func"},
		{Result: float64(42)},
	}
	data := &TriggerEventData{PrimaryKey: "pk-1"}

	result := resolveTemplateStringWithChain("pk=${event.primaryKey} r0=${effects[0].result} r1=${effects[1].result}", data, results)
	expected := "pk=pk-1 r0=value-from-func r1=42"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestResolveTemplateStringWithChain_NilResult(t *testing.T) {
	results := []EffectResult{
		{Result: nil},
	}
	data := &TriggerEventData{}

	result := resolveTemplateStringWithChain("r=${effects[0].result}", data, results)
	expected := "r=<nil>"
	if result != expected {
		t.Fatalf("expected %q, got %q", expected, result)
	}
}

func TestResolveTemplateStringWithChain_NoChainRefs(t *testing.T) {
	data := &TriggerEventData{PrimaryKey: "pk-1"}
	result := resolveTemplateStringWithChain("pk=${event.primaryKey}", data, nil)
	if result != "pk=pk-1" {
		t.Fatalf("expected 'pk=pk-1', got %q", result)
	}
}

// --- Notification effect tests ---

// mockNotificationCreator implements NotificationCreator for testing.
type mockNotificationCreator struct {
	mu    sync.Mutex
	calls []notificationCreatorCall
	err   error
}

type notificationCreatorCall struct {
	userID   string
	title    string
	body     string
	nType    string
	link     string
}

func (m *mockNotificationCreator) CreateNotificationForUser(ctx context.Context, userID, title, body, nType, link string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, notificationCreatorCall{
		userID: userID,
		title:  title,
		body:   body,
		nType:  nType,
		link:   link,
	})
	return m.err
}

func (m *mockNotificationCreator) getCalls() []notificationCreatorCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]notificationCreatorCall, len(m.calls))
	copy(result, m.calls)
	return result
}

func TestParseEffects_Notification(t *testing.T) {
	raw := json.RawMessage(`[{"type":"notification","channel":"platform","template":"Object ${event.primaryKey} created","recipients":["user1","user2"]}]`)
	effects, err := ParseEffects(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(effects) != 1 {
		t.Fatalf("expected 1 effect, got %d", len(effects))
	}
	if effects[0].Type != "notification" {
		t.Fatalf("expected type 'notification', got %q", effects[0].Type)
	}
	if effects[0].Channel != "platform" {
		t.Fatalf("expected channel 'platform', got %q", effects[0].Channel)
	}
	if effects[0].Template != "Object ${event.primaryKey} created" {
		t.Fatalf("expected template, got %q", effects[0].Template)
	}
	if len(effects[0].Recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %d", len(effects[0].Recipients))
	}
}

func TestProcessEffects_Notification_Platform(t *testing.T) {
	creator := &mockNotificationCreator{}
	data := &TriggerEventData{PrimaryKey: "emp-1", ObjectType: "Employee", EditType: "CREATE"}

	effects := json.RawMessage(`[{"type":"notification","channel":"platform","template":"New ${event.objectType}: ${event.primaryKey}","recipients":["alice","bob"]}]`)

	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, nil, creator)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := creator.getCalls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 notification calls (one per recipient), got %d", len(calls))
	}
	if calls[0].userID != "alice" {
		t.Fatalf("expected userID 'alice', got %q", calls[0].userID)
	}
	if calls[0].body != "New Employee: emp-1" {
		t.Fatalf("expected resolved body 'New Employee: emp-1', got %q", calls[0].body)
	}
	if calls[1].userID != "bob" {
		t.Fatalf("expected userID 'bob', got %q", calls[1].userID)
	}
}

func TestProcessEffects_Notification_NilCreator(t *testing.T) {
	data := &TriggerEventData{}
	effects := json.RawMessage(`[{"type":"notification","channel":"platform","template":"test","recipients":["user1"]}]`)

	// Should not panic or error when creator is nil — graceful skip
	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProcessEffects_Notification_Failure(t *testing.T) {
	creator := &mockNotificationCreator{err: fmt.Errorf("db error")}
	data := &TriggerEventData{}

	effects := json.RawMessage(`[{"type":"notification","channel":"platform","template":"test","recipients":["user1"]}]`)

	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, nil, creator)
	if err == nil {
		t.Fatal("expected error from failing notification")
	}
	if !contains(err.Error(), "db error") {
		t.Fatalf("expected error to contain 'db error', got %q", err.Error())
	}
}

func TestProcessEffects_Notification_EmailSkipsGracefully(t *testing.T) {
	// Email channel with no SMTP configured → graceful skip (no error)
	creator := &mockNotificationCreator{}
	data := &TriggerEventData{}

	effects := json.RawMessage(`[{"type":"notification","channel":"email","template":"hello","recipients":["user1"]}]`)

	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, nil, creator)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Email channel should not create platform notifications
	calls := creator.getCalls()
	if len(calls) != 0 {
		t.Fatalf("expected 0 platform calls for email channel, got %d", len(calls))
	}
}

func TestProcessEffects_Notification_TemplateResolution(t *testing.T) {
	creator := &mockNotificationCreator{}
	data := &TriggerEventData{
		PrimaryKey: "emp-42",
		EditType:   "MODIFY",
		ObjectType: "Employee",
		Properties: map[string]interface{}{
			"name": "Alice",
		},
	}

	effects := json.RawMessage(`[{"type":"notification","channel":"platform","template":"${event.properties.name} was updated (${event.editType})","recipients":["admin"]}]`)

	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, nil, creator)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := creator.getCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].body != "Alice was updated (MODIFY)" {
		t.Fatalf("expected resolved template, got %q", calls[0].body)
	}
}

func TestProcessEffects_Notification_NoRecipients(t *testing.T) {
	creator := &mockNotificationCreator{}
	data := &TriggerEventData{}

	effects := json.RawMessage(`[{"type":"notification","channel":"platform","template":"test","recipients":[]}]`)

	_, err := processEffects(context.Background(), effects, "ri.ontology.main.ontology.1", data, nil, nil, creator)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// No recipients → no calls
	if len(creator.getCalls()) != 0 {
		t.Fatal("expected 0 calls for empty recipients")
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
