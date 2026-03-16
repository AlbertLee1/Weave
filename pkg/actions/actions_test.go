package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// Mock OMS Repository
// ---------------------------------------------------------------------------

type mockOmsRepo struct {
	actionTypes []oms.ActionType
}

func (m *mockOmsRepo) CreateOntology(_ context.Context, _ *oms.Ontology) error   { return nil }
func (m *mockOmsRepo) GetOntology(_ context.Context, _ string) (*oms.Ontology, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) { return nil, nil }

func (m *mockOmsRepo) CreateObjectType(_ context.Context, _ *oms.ObjectType) error { return nil }
func (m *mockOmsRepo) GetObjectType(_ context.Context, _ string) (*oms.ObjectType, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetObjectTypeByAPIName(_ context.Context, _, _ string) (*oms.ObjectType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateObjectType(_ context.Context, _ *oms.ObjectType) error { return nil }
func (m *mockOmsRepo) DeleteObjectType(_ context.Context, _ string) error          { return nil }

func (m *mockOmsRepo) CreateProperty(_ context.Context, _ *oms.Property) error { return nil }
func (m *mockOmsRepo) ListProperties(_ context.Context, _ string) ([]oms.Property, error) {
	return nil, nil
}
func (m *mockOmsRepo) DeleteProperty(_ context.Context, _ string) error { return nil }

func (m *mockOmsRepo) CreateLinkType(_ context.Context, _ *oms.LinkType) error { return nil }
func (m *mockOmsRepo) GetLinkType(_ context.Context, _ string) (*oms.LinkType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListOutgoingLinkTypes(_ context.Context, _ string) ([]oms.LinkType, error) {
	return nil, nil
}

func (m *mockOmsRepo) CreateActionType(_ context.Context, _ *oms.ActionType) error { return nil }
func (m *mockOmsRepo) GetActionType(_ context.Context, _ string) (*oms.ActionType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListActionTypes(_ context.Context, _ string) ([]oms.ActionType, error) {
	return m.actionTypes, nil
}
func (m *mockOmsRepo) UpdateActionType(_ context.Context, _ *oms.ActionType) error { return nil }

func (m *mockOmsRepo) CreateInterface(_ context.Context, _ *oms.Interface) error { return nil }
func (m *mockOmsRepo) ListInterfaces(_ context.Context, _ string) ([]oms.Interface, error) {
	return nil, nil
}
func (m *mockOmsRepo) AttachInterface(_ context.Context, _ *oms.ObjectTypeInterface) error {
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func mustJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

func newTestActionType(apiName string, params []ParameterDef, rules []Rule) oms.ActionType {
	return oms.ActionType{
		RID:        "ri.ontology.main.action-type.test-" + apiName,
		APIName:    apiName,
		Parameters: mustJSON(params),
		Rules:      mustJSON(rules),
		Status:     "ACTIVE",
	}
}

// ---------------------------------------------------------------------------
// Validation Tests (1–7)
// ---------------------------------------------------------------------------

func TestValidateParameters_AllPresent(t *testing.T) {
	defs := []ParameterDef{
		{ID: "name", Type: "string", Required: true},
		{ID: "age", Type: "integer", Required: true},
	}
	params := map[string]interface{}{
		"name": "Alice",
		"age":  float64(30),
	}
	if err := ValidateParameters(defs, params); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateParameters_MissingRequired(t *testing.T) {
	defs := []ParameterDef{
		{ID: "name", Type: "string", Required: true},
	}
	params := map[string]interface{}{}
	err := ValidateParameters(defs, params)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
}

func TestValidateParameters_OptionalMissing(t *testing.T) {
	defs := []ParameterDef{
		{ID: "name", Type: "string", Required: true},
		{ID: "nickname", Type: "string", Required: false},
	}
	params := map[string]interface{}{
		"name": "Alice",
	}
	if err := ValidateParameters(defs, params); err != nil {
		t.Fatalf("expected no error for optional missing, got: %v", err)
	}
}

func TestValidateParameters_WrongType(t *testing.T) {
	defs := []ParameterDef{
		{ID: "age", Type: "integer", Required: true},
	}
	params := map[string]interface{}{
		"age": "not-a-number",
	}
	err := ValidateParameters(defs, params)
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}

func TestValidateParameters_UnknownParam(t *testing.T) {
	defs := []ParameterDef{
		{ID: "name", Type: "string", Required: true},
	}
	params := map[string]interface{}{
		"name":  "Alice",
		"extra": "oops",
	}
	err := ValidateParameters(defs, params)
	if err == nil {
		t.Fatal("expected error for unknown param")
	}
}

func TestParseParameterDefs_Valid(t *testing.T) {
	data := mustJSON([]ParameterDef{
		{ID: "x", Type: "string", Required: true},
		{ID: "y", Type: "integer", Required: false},
	})
	defs, err := ParseParameterDefs(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(defs) != 2 {
		t.Fatalf("expected 2 defs, got %d", len(defs))
	}
	if defs[0].ID != "x" || defs[1].ID != "y" {
		t.Fatalf("unexpected defs: %+v", defs)
	}
}

func TestParseParameterDefs_Empty(t *testing.T) {
	defs, err := ParseParameterDefs(json.RawMessage("[]"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if defs != nil {
		t.Fatalf("expected nil for empty array, got %+v", defs)
	}
}

// ---------------------------------------------------------------------------
// Rules Tests (8–15)
// ---------------------------------------------------------------------------

func TestParseRules_CreateObject(t *testing.T) {
	data := mustJSON([]Rule{
		{
			Type:       "createObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
	})
	rules, err := ParseRules(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 || rules[0].Type != "createObject" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
}

func TestParseRules_ModifyObject(t *testing.T) {
	data := mustJSON([]Rule{
		{
			Type:       "modifyObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"salary": {Type: "parameter", Value: "newSalary"},
			},
		},
	})
	rules, err := ParseRules(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 || rules[0].Type != "modifyObject" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
}

func TestParseRules_DeleteObject(t *testing.T) {
	data := mustJSON([]Rule{
		{Type: "deleteObject", ObjectType: "Employee"},
	})
	rules, err := ParseRules(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 || rules[0].Type != "deleteObject" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
}

func TestExecuteRules_CreateObject(t *testing.T) {
	rules := []Rule{
		{
			Type:       "createObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
	}
	params := map[string]interface{}{"name": "Alice"}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected CREATE, got %s", edits[0].Type)
	}
	if edits[0].ObjectType != "Employee" {
		t.Fatalf("expected Employee, got %s", edits[0].ObjectType)
	}
	if edits[0].PrimaryKey == "" {
		t.Fatal("expected non-empty primary key")
	}
	if edits[0].Properties["name"] != "Alice" {
		t.Fatalf("expected name=Alice, got %v", edits[0].Properties["name"])
	}
}

func TestExecuteRules_ModifyObject(t *testing.T) {
	rules := []Rule{
		{
			Type:       "modifyObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"salary": {Type: "parameter", Value: "newSalary"},
			},
		},
	}
	params := map[string]interface{}{
		"primaryKey": "emp-1",
		"newSalary":  float64(100000),
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeModify {
		t.Fatalf("expected MODIFY, got %s", edits[0].Type)
	}
	if edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected primaryKey emp-1, got %s", edits[0].PrimaryKey)
	}
}

func TestExecuteRules_DeleteObject(t *testing.T) {
	rules := []Rule{
		{Type: "deleteObject", ObjectType: "Employee"},
	}
	params := map[string]interface{}{
		"primaryKey": "emp-1",
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeDelete {
		t.Fatalf("expected DELETE, got %s", edits[0].Type)
	}
	if edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected primaryKey emp-1, got %s", edits[0].PrimaryKey)
	}
}

func TestExecuteRules_ParameterBinding(t *testing.T) {
	rules := []Rule{
		{
			Type:       "createObject",
			ObjectType: "Task",
			PropertyBindings: map[string]PropertyBinding{
				"title":    {Type: "parameter", Value: "taskTitle"},
				"priority": {Type: "parameter", Value: "taskPriority"},
			},
		},
	}
	params := map[string]interface{}{
		"taskTitle":    "Fix bug",
		"taskPriority": float64(1),
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edits[0].Properties["title"] != "Fix bug" {
		t.Fatalf("expected title=Fix bug, got %v", edits[0].Properties["title"])
	}
	if edits[0].Properties["priority"] != float64(1) {
		t.Fatalf("expected priority=1, got %v", edits[0].Properties["priority"])
	}
}

func TestExecuteRules_StaticBinding(t *testing.T) {
	rules := []Rule{
		{
			Type:       "createObject",
			ObjectType: "Task",
			PropertyBindings: map[string]PropertyBinding{
				"status": {Type: "static", Value: "OPEN"},
				"title":  {Type: "parameter", Value: "taskTitle"},
			},
		},
	}
	params := map[string]interface{}{
		"taskTitle": "Build feature",
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if edits[0].Properties["status"] != "OPEN" {
		t.Fatalf("expected status=OPEN, got %v", edits[0].Properties["status"])
	}
	if edits[0].Properties["title"] != "Build feature" {
		t.Fatalf("expected title=Build feature, got %v", edits[0].Properties["title"])
	}
}

// ---------------------------------------------------------------------------
// Collapse Tests (16–22)
// ---------------------------------------------------------------------------

func TestCollapseEdits_CreateDelete(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeDelete, ObjectType: "A", PrimaryKey: "1"},
	}
	result := CollapseEdits(edits)
	if len(result) != 0 {
		t.Fatalf("expected 0 edits after CREATE+DELETE, got %d", len(result))
	}
}

func TestCollapseEdits_CreateModify(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"y": 2}},
	}
	result := CollapseEdits(edits)
	if len(result) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result))
	}
	if result[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected CREATE, got %s", result[0].Type)
	}
	if result[0].Properties["x"] != 1 || result[0].Properties["y"] != 2 {
		t.Fatalf("expected merged properties {x:1, y:2}, got %v", result[0].Properties)
	}
}

func TestCollapseEdits_ModifyModify(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"x": 2, "y": 3}},
	}
	result := CollapseEdits(edits)
	if len(result) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result))
	}
	if result[0].Properties["x"] != 2 {
		t.Fatalf("expected x=2, got %v", result[0].Properties["x"])
	}
	if result[0].Properties["y"] != 3 {
		t.Fatalf("expected y=3, got %v", result[0].Properties["y"])
	}
}

func TestCollapseEdits_ModifyDelete(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeDelete, ObjectType: "A", PrimaryKey: "1"},
	}
	result := CollapseEdits(edits)
	if len(result) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result))
	}
	if result[0].Type != funnel.EditTypeDelete {
		t.Fatalf("expected DELETE, got %s", result[0].Type)
	}
}

func TestCollapseEdits_NoCollapse(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeCreate, ObjectType: "B", PrimaryKey: "2", Properties: map[string]interface{}{"y": 2}},
	}
	result := CollapseEdits(edits)
	if len(result) != 2 {
		t.Fatalf("expected 2 edits (no collapse), got %d", len(result))
	}
}

func TestCollapseEdits_PreservesOrder(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "B", PrimaryKey: "2", Properties: map[string]interface{}{}},
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{}},
		{Type: funnel.EditTypeCreate, ObjectType: "C", PrimaryKey: "3", Properties: map[string]interface{}{}},
	}
	result := CollapseEdits(edits)
	if len(result) != 3 {
		t.Fatalf("expected 3 edits, got %d", len(result))
	}
	if result[0].ObjectType != "B" || result[1].ObjectType != "A" || result[2].ObjectType != "C" {
		t.Fatalf("order not preserved: %s, %s, %s", result[0].ObjectType, result[1].ObjectType, result[2].ObjectType)
	}
}

func TestCollapseEdits_Empty(t *testing.T) {
	result := CollapseEdits(nil)
	if len(result) != 0 {
		t.Fatalf("expected 0 edits for empty input, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Executor Tests (23–27)
// ---------------------------------------------------------------------------

func TestExecutor_Apply_Success(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ActionRID == "" {
		t.Fatal("expected non-empty ActionRID")
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected CREATE edit, got %s", result.Edits[0].Type)
	}
}

func TestExecutor_Apply_ActionNotFound(t *testing.T) {
	repo := &mockOmsRepo{actionTypes: nil}
	exec := NewExecutor(repo, nil)
	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "nonExistent",
		Parameters: map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected error for unknown action type")
	}
}

func TestExecutor_Apply_ValidationError(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{Type: "createObject", ObjectType: "Employee"},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{}, // missing required "name"
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestExecutor_Apply_EmptyEdits(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("noopAction", nil, nil),
		},
	}
	exec := NewExecutor(repo, nil)
	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "noopAction",
		Parameters: map[string]interface{}{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Edits != nil {
		t.Fatalf("expected nil edits, got %d", len(result.Edits))
	}
}

func TestExecutor_Apply_MultipleRules(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("transferEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "newDept", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "modifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"department": {Type: "parameter", Value: "newDept"},
					},
				},
				{
					Type:       "createObject",
					ObjectType: "TransferLog",
					PropertyBindings: map[string]PropertyBinding{
						"employeeId": {Type: "parameter", Value: "primaryKey"},
						"department": {Type: "parameter", Value: "newDept"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "transferEmployee",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-1",
			"newDept":    "Engineering",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeModify {
		t.Fatalf("expected first edit MODIFY, got %s", result.Edits[0].Type)
	}
	if result.Edits[1].Type != funnel.EditTypeCreate {
		t.Fatalf("expected second edit CREATE, got %s", result.Edits[1].Type)
	}
}

// ---------------------------------------------------------------------------
// Handler Tests (28–30)
// ---------------------------------------------------------------------------

func setupRouter(handler *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/apply", handler.Apply)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/applyBatch", handler.ApplyBatch)
	return r
}

func TestHandler_Apply_200(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Bob"},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result ApplyResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit in response, got %d", len(result.Edits))
	}
}

func TestHandler_Apply_MissingActionType(t *testing.T) {
	repo := &mockOmsRepo{}
	exec := NewExecutor(repo, nil)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandler_ApplyBatch_200(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("createEmployee", []ParameterDef{
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"actions": []ApplyRequest{
			{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Alice"}},
			{ActionType: "createEmployee", Parameters: map[string]interface{}{"name": "Bob"}},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/applyBatch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	var results []ApplyResult
	if err := json.Unmarshal(resp["results"], &results); err != nil {
		t.Fatalf("unmarshal results: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}
