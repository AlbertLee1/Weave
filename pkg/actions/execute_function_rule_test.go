package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// recordingFunctionDispatcher captures every Dispatch call so multi-rule
// executeFunction tests can assert per-call inputs (FunctionRID, params).
type recordingFunctionDispatcher struct {
	calls         []recordedDispatch
	editsByFnRID  map[string][]funnel.Edit
	errByFnRID    map[string]error
	defaultEdits  []funnel.Edit
	defaultErrFn  func(string) error
}

type recordedDispatch struct {
	functionRID string
	apiName     string
	rid         string
	params      map[string]interface{}
}

func (d *recordingFunctionDispatcher) Dispatch(_ context.Context, at *oms.ActionType, params map[string]interface{}) ([]funnel.Edit, error) {
	rec := recordedDispatch{params: params}
	if at != nil {
		rec.functionRID = at.FunctionRID
		rec.apiName = at.APIName
		rec.rid = at.RID
	}
	d.calls = append(d.calls, rec)
	if err, ok := d.errByFnRID[rec.functionRID]; ok {
		return nil, err
	}
	if d.defaultErrFn != nil {
		if err := d.defaultErrFn(rec.functionRID); err != nil {
			return nil, err
		}
	}
	if edits, ok := d.editsByFnRID[rec.functionRID]; ok {
		return edits, nil
	}
	return d.defaultEdits, nil
}

// ── ParseRules / ExecuteRules ──

func TestParseRules_ExecuteFunction(t *testing.T) {
	data := json.RawMessage(`[{"type":"executeFunction","functionRid":"ri.functions.main.function.foo"}]`)
	rules, err := ParseRules(data)
	if err != nil {
		t.Fatalf("ParseRules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Type != "executeFunction" {
		t.Fatalf("expected executeFunction, got %q", rules[0].Type)
	}
	if rules[0].FunctionRID != "ri.functions.main.function.foo" {
		t.Fatalf("expected FunctionRID set, got %q", rules[0].FunctionRID)
	}
	if !rules[0].IsExecuteFunction() {
		t.Fatal("IsExecuteFunction() should be true")
	}
}

func TestExecuteRules_SkipsExecuteFunction(t *testing.T) {
	rules := []Rule{
		{Type: "executeFunction", FunctionRID: "ri.functions.main.function.foo"},
		{
			Type:       "createObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "static", Value: "Alice"},
			},
		},
		{Type: "executeFunction", FunctionRID: "ri.functions.main.function.bar"},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{})
	if err != nil {
		t.Fatalf("ExecuteRules: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit (executeFunction skipped), got %d", len(edits))
	}
	if edits[0].ObjectType != "Employee" {
		t.Fatalf("expected Employee, got %q", edits[0].ObjectType)
	}
}

// ── Executor.Apply with executeFunction rules ──

func TestExecutor_Apply_ExecuteFunctionRule_Dispatches(t *testing.T) {
	at := newTestActionType("createWithFn", []ParameterDef{
		{ID: "name", Type: "string", Required: true},
	}, []Rule{
		{Type: "executeFunction", FunctionRID: "ri.functions.main.function.create-emp"},
	})

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	disp := &recordingFunctionDispatcher{
		defaultEdits: []funnel.Edit{{
			Type:       funnel.EditTypeCreate,
			ObjectType: "Employee",
			PrimaryKey: "emp-from-fn",
			Properties: map[string]interface{}{"name": "Alice"},
		}},
	}
	exec.SetFunctionDispatcher(disp)

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createWithFn",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(disp.calls) != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", len(disp.calls))
	}
	if disp.calls[0].functionRID != "ri.functions.main.function.create-emp" {
		t.Errorf("dispatch FunctionRID: got %q", disp.calls[0].functionRID)
	}
	if disp.calls[0].apiName != "createWithFn" {
		t.Errorf("dispatch APIName: got %q", disp.calls[0].apiName)
	}
	if disp.calls[0].params["name"] != "Alice" {
		t.Errorf("dispatch params: got %v", disp.calls[0].params)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].PrimaryKey != "emp-from-fn" {
		t.Errorf("expected fn-derived edit, got pk %q", result.Edits[0].PrimaryKey)
	}
	if result.Edits[0].Source != funnel.EditSourceUser {
		t.Errorf("expected user-source tag, got %q", result.Edits[0].Source)
	}
}

func TestExecutor_Apply_MixedRules_AppendsFunctionEditsAfter(t *testing.T) {
	at := newTestActionType("mixed", []ParameterDef{
		{ID: "name", Type: "string", Required: true},
	}, []Rule{
		{
			Type:       "createObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
		{Type: "executeFunction", FunctionRID: "ri.functions.main.function.audit"},
	})

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	disp := &recordingFunctionDispatcher{
		defaultEdits: []funnel.Edit{{
			Type:       funnel.EditTypeCreate,
			ObjectType: "AuditLog",
			PrimaryKey: "log-1",
			Properties: map[string]interface{}{"action": "created"},
		}},
	}
	exec.SetFunctionDispatcher(disp)

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "mixed",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Edits) != 2 {
		t.Fatalf("expected 2 edits (rule + fn), got %d", len(result.Edits))
	}
	// Rule-derived edit comes first.
	if result.Edits[0].ObjectType != "Employee" {
		t.Errorf("edit[0]: expected Employee, got %q", result.Edits[0].ObjectType)
	}
	if result.Edits[0].Properties["name"] != "Alice" {
		t.Errorf("edit[0].name: got %v", result.Edits[0].Properties["name"])
	}
	// Function-derived edit comes after.
	if result.Edits[1].ObjectType != "AuditLog" {
		t.Errorf("edit[1]: expected AuditLog, got %q", result.Edits[1].ObjectType)
	}
	// Both edits tagged user-source.
	for i, e := range result.Edits {
		if e.Source != funnel.EditSourceUser {
			t.Errorf("edit[%d].Source: got %q", i, e.Source)
		}
	}
}

func TestExecutor_Apply_MultipleExecuteFunctionRules_AllDispatched(t *testing.T) {
	at := newTestActionType("multi", nil, []Rule{
		{Type: "executeFunction", FunctionRID: "ri.functions.main.function.first"},
		{Type: "executeFunction", FunctionRID: "ri.functions.main.function.second"},
	})

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	disp := &recordingFunctionDispatcher{
		editsByFnRID: map[string][]funnel.Edit{
			"ri.functions.main.function.first": {{
				Type:       funnel.EditTypeCreate,
				ObjectType: "A",
				PrimaryKey: "a-1",
			}},
			"ri.functions.main.function.second": {
				{Type: funnel.EditTypeCreate, ObjectType: "B", PrimaryKey: "b-1"},
				{Type: funnel.EditTypeCreate, ObjectType: "B", PrimaryKey: "b-2"},
			},
		},
	}
	exec.SetFunctionDispatcher(disp)

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "multi",
		Parameters: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(disp.calls) != 2 {
		t.Fatalf("expected 2 dispatch calls, got %d", len(disp.calls))
	}
	if disp.calls[0].functionRID != "ri.functions.main.function.first" ||
		disp.calls[1].functionRID != "ri.functions.main.function.second" {
		t.Fatalf("dispatch order broken: %+v", disp.calls)
	}
	if len(result.Edits) != 3 {
		t.Fatalf("expected 3 edits (1 + 2), got %d", len(result.Edits))
	}
	if result.Edits[0].ObjectType != "A" {
		t.Errorf("edit[0]: got %q", result.Edits[0].ObjectType)
	}
	if result.Edits[1].ObjectType != "B" || result.Edits[1].PrimaryKey != "b-1" {
		t.Errorf("edit[1]: got %+v", result.Edits[1])
	}
	if result.Edits[2].PrimaryKey != "b-2" {
		t.Errorf("edit[2]: got %+v", result.Edits[2])
	}
}

func TestExecutor_Apply_ExecuteFunctionRule_EmptyFunctionRID_Errors(t *testing.T) {
	at := newTestActionType("badRule", nil, []Rule{
		{Type: "executeFunction", FunctionRID: ""},
	})

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	exec.SetFunctionDispatcher(&recordingFunctionDispatcher{})

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "badRule",
		Parameters: map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error for empty FunctionRID")
	}
	if !strings.Contains(err.Error(), "functionRid is required") {
		t.Errorf("expected functionRid error, got: %v", err)
	}
}

func TestExecutor_Apply_ExecuteFunctionRule_NoDispatcher_Errors(t *testing.T) {
	at := newTestActionType("noDispatcher", nil, []Rule{
		{Type: "executeFunction", FunctionRID: "ri.functions.main.function.x"},
	})

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	// No SetFunctionDispatcher call.

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "noDispatcher",
		Parameters: map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error when dispatcher is missing")
	}
	if !strings.Contains(err.Error(), "function dispatcher not configured") {
		t.Errorf("expected dispatcher-missing error, got: %v", err)
	}
}

func TestExecutor_Apply_ExecuteFunctionRule_DispatcherError_Propagates(t *testing.T) {
	at := newTestActionType("dispatchFails", nil, []Rule{
		{Type: "executeFunction", FunctionRID: "ri.functions.main.function.bad"},
	})

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	exec.SetFunctionDispatcher(&recordingFunctionDispatcher{
		errByFnRID: map[string]error{
			"ri.functions.main.function.bad": fmt.Errorf("function blew up"),
		},
	})

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "dispatchFails",
		Parameters: map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected dispatcher error to propagate")
	}
	if !strings.Contains(err.Error(), "function blew up") {
		t.Errorf("expected propagated error message, got: %v", err)
	}
}

// TestExecutor_Apply_ExecuteFunctionRule_DispatcherReceivesActionTypeMetadata
// asserts the ActionType envelope passed to the dispatcher carries the
// rule-level FunctionRID override while preserving the action type's identity
// fields (RID, APIName) — function authors program against those identifiers
// regardless of whether the call originated from a top-level
// IsFunctionBacked action or from an executeFunction rule.
func TestExecutor_Apply_ExecuteFunctionRule_DispatcherReceivesActionTypeMetadata(t *testing.T) {
	at := newTestActionType("envelopeCheck", nil, []Rule{
		{Type: "executeFunction", FunctionRID: "ri.functions.main.function.fn-A"},
	})
	// The action type itself has a different FunctionRID (legacy field). The
	// rule's FunctionRID must win.
	at.FunctionRID = "ri.functions.main.function.legacy-default"

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	disp := &recordingFunctionDispatcher{}
	exec.SetFunctionDispatcher(disp)

	if _, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "envelopeCheck",
		Parameters: map[string]interface{}{},
	}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if len(disp.calls) != 1 {
		t.Fatalf("expected 1 dispatch call, got %d", len(disp.calls))
	}
	got := disp.calls[0]
	if got.functionRID != "ri.functions.main.function.fn-A" {
		t.Errorf("dispatcher FunctionRID: got %q, want fn-A (rule wins over legacy default)", got.functionRID)
	}
	if got.apiName != "envelopeCheck" {
		t.Errorf("dispatcher APIName: got %q", got.apiName)
	}
	if got.rid != at.RID {
		t.Errorf("dispatcher RID: got %q, want %q", got.rid, at.RID)
	}

	// Verify the original action type's FunctionRID was NOT mutated by the
	// shallow-copy override.
	if at.FunctionRID != "ri.functions.main.function.legacy-default" {
		t.Errorf("original action type FunctionRID was mutated: %q", at.FunctionRID)
	}
}
