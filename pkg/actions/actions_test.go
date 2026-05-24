package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
)

// ---------------------------------------------------------------------------
// Mock OMS Repository
// ---------------------------------------------------------------------------

type mockOmsRepo struct {
	actionTypes  []oms.ActionType
	insertedLogs []*oms.ActionLog
	insertLogErr error
	// US-023: optional per-test optimistic-concurrency overrides. When these
	// maps are non-nil the mock returns the configured ObjectType / version
	// for ExpectedVersion checks; otherwise the default nil/0 values
	// preserve the legacy stub behaviour for all pre-existing tests.
	objectTypesByAPIName map[string]*oms.ObjectType
	objectVersionCounts  map[string]int64
	// US-105: action log lookup for revert tests.
	actionLogByID map[int64]*oms.ActionLog
	// Round-33 side-effect DLQ rows captured by InsertSideEffectDLQRow.
	dlqRows []*oms.SideEffectDLQRow
}

func (m *mockOmsRepo) CreateOntology(_ context.Context, _ *oms.Ontology) error { return nil }
func (m *mockOmsRepo) GetOntology(_ context.Context, _ string) (*oms.Ontology, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListOntologies(_ context.Context) ([]oms.Ontology, error) { return nil, nil }
func (m *mockOmsRepo) UpdateOntology(_ context.Context, _ *oms.Ontology) error  { return nil }

func (m *mockOmsRepo) CreateObjectType(_ context.Context, _ *oms.ObjectType) error { return nil }
func (m *mockOmsRepo) GetObjectType(_ context.Context, _ string) (*oms.ObjectType, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetObjectTypeByAPIName(_ context.Context, _, apiName string) (*oms.ObjectType, error) {
	if m.objectTypesByAPIName != nil {
		if ot, ok := m.objectTypesByAPIName[apiName]; ok {
			return ot, nil
		}
	}
	return nil, nil
}
func (m *mockOmsRepo) ListObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateObjectType(_ context.Context, _ *oms.ObjectType) error { return nil }
func (m *mockOmsRepo) DeleteObjectType(_ context.Context, _ string) error          { return nil }

func (m *mockOmsRepo) CreateProperty(_ context.Context, _ *oms.Property) error { return nil }
func (m *mockOmsRepo) GetProperty(_ context.Context, _ string) (*oms.Property, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListProperties(_ context.Context, _ string) ([]oms.Property, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateProperty(_ context.Context, _ *oms.Property) error { return nil }
func (m *mockOmsRepo) DeleteProperty(_ context.Context, _ string) error        { return nil }

func (m *mockOmsRepo) CreateLinkType(_ context.Context, _ *oms.LinkType) error { return nil }
func (m *mockOmsRepo) GetLinkType(_ context.Context, _ string) (*oms.LinkType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListOutgoingLinkTypes(_ context.Context, _ string) ([]oms.LinkType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListIncomingLinkTypes(_ context.Context, _ string) ([]oms.LinkType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListLinkTypes(_ context.Context, _ string) ([]oms.LinkType, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateLinkType(_ context.Context, _ *oms.LinkType) error { return nil }
func (m *mockOmsRepo) DeleteLinkType(_ context.Context, _ string) error        { return nil }

func (m *mockOmsRepo) CreateActionType(_ context.Context, _ *oms.ActionType) error { return nil }
func (m *mockOmsRepo) GetActionType(_ context.Context, _ string) (*oms.ActionType, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetActionTypeByAPIName(_ context.Context, _, _ string) (*oms.ActionType, error) {
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) ListActionTypes(_ context.Context, _ string) ([]oms.ActionType, error) {
	return m.actionTypes, nil
}
func (m *mockOmsRepo) UpdateActionType(_ context.Context, _ *oms.ActionType) error { return nil }
func (m *mockOmsRepo) DeleteActionType(_ context.Context, _ string) error          { return nil }

func (m *mockOmsRepo) CreateInterface(_ context.Context, _ *oms.Interface) error { return nil }
func (m *mockOmsRepo) GetInterface(_ context.Context, _ string) (*oms.Interface, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetInterfaceByAPIName(_ context.Context, _, _ string) (*oms.Interface, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListInterfaces(_ context.Context, _ string) ([]oms.Interface, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateInterface(_ context.Context, _ *oms.Interface) error { return nil }
func (m *mockOmsRepo) DeleteInterface(_ context.Context, _ string) error         { return nil }
func (m *mockOmsRepo) ListInterfaceObjectTypes(_ context.Context, _ string) ([]oms.ObjectType, error) {
	return nil, nil
}
func (m *mockOmsRepo) AttachInterface(_ context.Context, _ *oms.ObjectTypeInterface) error {
	return nil
}
func (m *mockOmsRepo) DetachInterface(_ context.Context, _, _ string) error { return nil }
func (m *mockOmsRepo) ListObjectTypeInterfaces(_ context.Context, _ string) ([]oms.ObjectTypeInterface, error) {
	return nil, nil
}

// SharedProperty stubs
func (m *mockOmsRepo) CreateSharedProperty(_ context.Context, _ *oms.SharedProperty) error {
	return nil
}
func (m *mockOmsRepo) GetSharedProperty(_ context.Context, _ string) (*oms.SharedProperty, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListSharedProperties(_ context.Context, _ string) ([]oms.SharedProperty, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateSharedProperty(_ context.Context, _ *oms.SharedProperty) error {
	return nil
}
func (m *mockOmsRepo) DeleteSharedProperty(_ context.Context, _ string) error { return nil }

// TypeGroup stubs
func (m *mockOmsRepo) CreateTypeGroup(_ context.Context, _ *oms.TypeGroup) error { return nil }
func (m *mockOmsRepo) GetTypeGroup(_ context.Context, _ string) (*oms.TypeGroup, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListTypeGroups(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateTypeGroup(_ context.Context, _ *oms.TypeGroup) error { return nil }
func (m *mockOmsRepo) DeleteTypeGroup(_ context.Context, _ string) error         { return nil }
func (m *mockOmsRepo) AssignTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (m *mockOmsRepo) RemoveTypeGroup(_ context.Context, _, _ string) error      { return nil }
func (m *mockOmsRepo) ListTypeGroupsForObjectType(_ context.Context, _ string) ([]oms.TypeGroup, error) {
	return nil, nil
}

// ValueType stubs
func (m *mockOmsRepo) CreateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (m *mockOmsRepo) GetValueType(_ context.Context, _ string) (*oms.ValueType, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetValueTypeByAPIName(_ context.Context, _ string) (*oms.ValueType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListValueTypes(_ context.Context) ([]oms.ValueType, error) { return nil, nil }
func (m *mockOmsRepo) UpdateValueType(_ context.Context, _ *oms.ValueType) error { return nil }
func (m *mockOmsRepo) DeleteValueType(_ context.Context, _ string) error         { return nil }
func (m *mockOmsRepo) ListPropertyUsagesByBaseType(_ context.Context, _ string) ([]oms.PropertyUsage, error) {
	return nil, nil
}

// DatasourceBinding stubs
func (m *mockOmsRepo) CreateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (m *mockOmsRepo) GetDatasourceBinding(_ context.Context, _ string) (*oms.DatasourceBinding, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListDatasourceBindings(_ context.Context, _ string) ([]oms.DatasourceBinding, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateDatasourceBinding(_ context.Context, _ *oms.DatasourceBinding) error {
	return nil
}
func (m *mockOmsRepo) DeleteDatasourceBinding(_ context.Context, _ string) error { return nil }

// QueryType stubs
func (m *mockOmsRepo) CreateQueryType(_ context.Context, _ *oms.QueryType) error { return nil }
func (m *mockOmsRepo) GetQueryType(_ context.Context, _ string) (*oms.QueryType, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetQueryTypeByAPIName(_ context.Context, _, _ string) (*oms.QueryType, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListQueryTypes(_ context.Context, _ string) ([]oms.QueryType, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateQueryType(_ context.Context, _ *oms.QueryType) error { return nil }
func (m *mockOmsRepo) DeleteQueryType(_ context.Context, _ string) error         { return nil }

// ActionLog stubs
func (m *mockOmsRepo) InsertActionLog(_ context.Context, log *oms.ActionLog) error {
	if m.insertLogErr != nil {
		return m.insertLogErr
	}
	// Back-fill an auto-incrementing ID so callers depending on the row's
	// primary-key (e.g. lineage upstream RID) can read a non-zero value
	// from the same pointer they handed to this stub.
	log.ID = int64(len(m.insertedLogs)) + 1
	m.insertedLogs = append(m.insertedLogs, log)
	// Mirror into actionLogByID so UpdateActionLog* lookups land on the
	// same pointer the test holds — needed for the round-32 side-effect
	// status persistence path which Update-after-Inserts.
	if m.actionLogByID == nil {
		m.actionLogByID = map[int64]*oms.ActionLog{}
	}
	m.actionLogByID[log.ID] = log
	return nil
}
func (m *mockOmsRepo) ListActionLogs(_ context.Context, _ string, _, _ int) ([]oms.ActionLog, error) {
	return nil, nil
}
func (m *mockOmsRepo) CountActionLogs(_ context.Context, _ string) (int, error) { return 0, nil }
func (m *mockOmsRepo) GetActionLog(_ context.Context, id int64) (*oms.ActionLog, error) {
	if m.actionLogByID != nil {
		if al, ok := m.actionLogByID[id]; ok {
			return al, nil
		}
	}
	return nil, oms.ErrNotFound
}
func (m *mockOmsRepo) UpdateActionLogStatus(_ context.Context, id int64, status string) error {
	if m.actionLogByID != nil {
		if al, ok := m.actionLogByID[id]; ok {
			al.Status = status
			return nil
		}
	}
	return oms.ErrNotFound
}
func (m *mockOmsRepo) UpdateActionLogSideEffectStatus(_ context.Context, id int64, status json.RawMessage) error {
	if m.actionLogByID != nil {
		if al, ok := m.actionLogByID[id]; ok {
			al.SideEffectStatus = status
			return nil
		}
	}
	return oms.ErrNotFound
}

// Side-effect DLQ stubs (PRD-V2 Gap-A4 round 33). The mock records
// inserted DLQ rows in dlqRows so the round-33 executor BDD can assert
// the wiring fires for failed outcomes.
func (m *mockOmsRepo) InsertSideEffectDLQRow(_ context.Context, row *oms.SideEffectDLQRow) error {
	m.dlqRows = append(m.dlqRows, row)
	return nil
}
func (m *mockOmsRepo) ListSideEffectDLQByActionLog(_ context.Context, actionLogID int64) ([]oms.SideEffectDLQRow, error) {
	out := []oms.SideEffectDLQRow{}
	for _, r := range m.dlqRows {
		if r != nil && r.ActionLogID == actionLogID {
			out = append(out, *r)
		}
	}
	return out, nil
}
func (m *mockOmsRepo) ListPendingSideEffectDLQRows(_ context.Context, _ int) ([]oms.SideEffectDLQRow, error) {
	return nil, nil
}
func (m *mockOmsRepo) MarkSideEffectDLQAbandoned(_ context.Context, _ int64) error { return nil }

// ObjectHistory stubs (Tier 2.3)
func (m *mockOmsRepo) InsertObjectHistory(_ context.Context, _ *oms.ObjectHistory) error {
	return nil
}
func (m *mockOmsRepo) ListObjectHistory(_ context.Context, _, _ string, _ int) ([]oms.ObjectHistory, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetObjectVersionCount(_ context.Context, objectTypeRID, primaryKey string) (int64, error) {
	if m.objectVersionCounts != nil {
		if v, ok := m.objectVersionCounts[objectTypeRID+"|"+primaryKey]; ok {
			return v, nil
		}
	}
	return 0, nil
}

// Search stubs
func (m *mockOmsRepo) SearchOntologyResources(_ context.Context, _, _ string) ([]oms.SearchResult, error) {
	return nil, nil
}

// Snapshot stubs
func (m *mockOmsRepo) CreateSnapshot(_ context.Context, _ *oms.OntologySnapshot) error { return nil }
func (m *mockOmsRepo) ListSnapshots(_ context.Context, _ string) ([]oms.OntologySnapshot, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetSnapshot(_ context.Context, _ string, _ int) (*oms.OntologySnapshot, error) {
	return nil, nil
}
func (m *mockOmsRepo) GetOntologyVersion(_ context.Context, _ string) (int, error) { return 0, nil }
func (m *mockOmsRepo) IncrementOntologyVersion(_ context.Context, _ string) (int, error) {
	return 1, nil
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
// UserID & ActionLog Tests
// ---------------------------------------------------------------------------

func TestExecutor_Apply_ExtractsUserIDFromContext(t *testing.T) {
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

	// Inject auth user into context
	ctx := auth.WithUser(context.Background(), &auth.User{ID: "alice", Roles: []string{"admin"}})
	_, err := exec.Apply(ctx, "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
	if repo.insertedLogs[0].UserID != "alice" {
		t.Fatalf("expected UserID 'alice', got %q", repo.insertedLogs[0].UserID)
	}
}

func TestExecutor_Apply_FallsBackToSystemUser(t *testing.T) {
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

	// No auth user in context
	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Bob"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
	if repo.insertedLogs[0].UserID != "system" {
		t.Fatalf("expected UserID 'system', got %q", repo.insertedLogs[0].UserID)
	}
}

func TestExecutor_Apply_WritesActionLog(t *testing.T) {
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

	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
	log := repo.insertedLogs[0]
	if log.ActionTypeRID != result.ActionRID {
		t.Fatalf("expected ActionTypeRID %q, got %q", result.ActionRID, log.ActionTypeRID)
	}
	if log.Status != "SUCCESS" {
		t.Fatalf("expected status SUCCESS, got %q", log.Status)
	}
}

func TestExecutor_Apply_ActionLogErrorNonFatal(t *testing.T) {
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
		insertLogErr: fmt.Errorf("db connection lost"),
	}
	exec := NewExecutor(repo, nil)

	// Action should still succeed even if log insert fails
	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("expected no error (log failure is non-fatal), got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Handler Tests (28–30)
// ---------------------------------------------------------------------------

func setupRouter(handler *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/apply", handler.Apply)
	r.Post("/api/v2/ontologies/{ontologyApiName}/actions/{action}/applyBatch", handler.ApplyBatch)
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

	// Foundry v2 body carries only parameters; action API name is in path.
	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{"name": "Bob"},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/createEmployee/apply", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result SyncApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result.Edits == nil {
		t.Fatal("expected edits in SyncApplyActionResponseV2")
	}
	if result.Edits.AddedObjectCount != 1 {
		t.Fatalf("expected addedObjectCount=1, got %d", result.Edits.AddedObjectCount)
	}
}

// TestHandler_Apply_BodyActionTypeIsIgnored verifies that, under the new
// path-driven schema, a stale `actionType` field in the request body is
// silently overridden by the URL's {action} segment. This locks the
// rip-and-replace: the path is the only source of truth.
func TestHandler_Apply_BodyActionTypeIsIgnored(t *testing.T) {
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

	// Body carries a DIFFERENT actionType than the path segment. The
	// handler must resolve the path value, not the body value.
	body := mustJSON(map[string]interface{}{
		"actionType": "bogus-stale-field",
		"parameters": map[string]interface{}{"name": "Carol"},
	})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createEmployee/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (body actionType should be ignored), got %d: %s",
			w.Code, w.Body.String())
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

	// Foundry batch is one-action-many-parameter-sets; the action is in
	// the path. Body items only need parameters.
	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"name": "Alice"}},
			{"parameters": map[string]interface{}{"name": "Bob"}},
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/ont-1/actions/createEmployee/applyBatch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp BatchApplyActionResponseV2
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Edits == nil {
		t.Fatal("expected edits in BatchApplyActionResponseV2")
	}
	if resp.Edits.AddedObjectCount != 2 {
		t.Fatalf("expected addedObjectCount=2, got %d", resp.Edits.AddedObjectCount)
	}
}

// ---------------------------------------------------------------------------
// Submission Criteria Tests (31–38)
// ---------------------------------------------------------------------------

func TestEvaluateCriteria_Nil(t *testing.T) {
	if err := EvaluateCriteria(nil, ActionContext{}); err != nil {
		t.Fatalf("expected nil for nil criteria, got: %v", err)
	}
}

func TestEvaluateCriteria_Empty(t *testing.T) {
	if err := EvaluateCriteria(json.RawMessage("[]"), ActionContext{}); err != nil {
		t.Fatalf("expected nil for empty array criteria, got: %v", err)
	}
}

func TestEvaluateCriteria_Always(t *testing.T) {
	criteria := mustJSON(SubmissionCriteria{Type: "always"})
	if err := EvaluateCriteria(criteria, ActionContext{}); err != nil {
		t.Fatalf("expected nil for 'always' criteria, got: %v", err)
	}
}

func TestEvaluateCriteria_ParameterMatch_Equal_Pass(t *testing.T) {
	criteria := mustJSON([]SubmissionCriteria{
		{
			Type: "parameterMatch",
			Value: mustJSON(map[string]interface{}{
				"parameter": "status",
				"operator":  "eq",
				"value":     "ACTIVE",
			}),
		},
	})
	ctx := ActionContext{Parameters: map[string]interface{}{"status": "ACTIVE"}}
	if err := EvaluateCriteria(criteria, ctx); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestEvaluateCriteria_ParameterMatch_Equal_Fail(t *testing.T) {
	criteria := mustJSON([]SubmissionCriteria{
		{
			Type: "parameterMatch",
			Value: mustJSON(map[string]interface{}{
				"parameter": "status",
				"operator":  "eq",
				"value":     "ACTIVE",
			}),
		},
	})
	ctx := ActionContext{Parameters: map[string]interface{}{"status": "INACTIVE"}}
	if err := EvaluateCriteria(criteria, ctx); err == nil {
		t.Fatal("expected error for mismatched parameterMatch, got nil")
	}
}

func TestEvaluateCriteria_ParameterMatch_Numeric_Pass(t *testing.T) {
	criteria := mustJSON(SubmissionCriteria{
		Type: "parameterMatch",
		Value: mustJSON(map[string]interface{}{
			"parameter": "age",
			"operator":  "gte",
			"value":     float64(18),
		}),
	})
	ctx := ActionContext{Parameters: map[string]interface{}{"age": float64(21)}}
	if err := EvaluateCriteria(criteria, ctx); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestEvaluateCriteria_ParameterMatch_Numeric_Fail(t *testing.T) {
	criteria := mustJSON(SubmissionCriteria{
		Type: "parameterMatch",
		Value: mustJSON(map[string]interface{}{
			"parameter": "age",
			"operator":  "gte",
			"value":     float64(18),
		}),
	})
	ctx := ActionContext{Parameters: map[string]interface{}{"age": float64(16)}}
	if err := EvaluateCriteria(criteria, ctx); err == nil {
		t.Fatal("expected error for age < 18, got nil")
	}
}

func TestEvaluateCriteria_MissingParameter(t *testing.T) {
	criteria := mustJSON(SubmissionCriteria{
		Type: "parameterMatch",
		Value: mustJSON(map[string]interface{}{
			"parameter": "status",
			"operator":  "eq",
			"value":     "ACTIVE",
		}),
	})
	ctx := ActionContext{Parameters: map[string]interface{}{}}
	if err := EvaluateCriteria(criteria, ctx); err == nil {
		t.Fatal("expected error for missing parameter, got nil")
	}
}

func TestEvaluateCriteria_UnknownType(t *testing.T) {
	criteria := mustJSON(SubmissionCriteria{Type: "userMatch"})
	if err := EvaluateCriteria(criteria, ActionContext{}); err == nil {
		t.Fatal("expected error for unknown criteria type, got nil")
	}
}

func TestExecutor_Apply_CriteriaBlocked(t *testing.T) {
	criteria := mustJSON([]SubmissionCriteria{
		{
			Type: "parameterMatch",
			Value: mustJSON(map[string]interface{}{
				"parameter": "role",
				"operator":  "eq",
				"value":     "admin",
			}),
		},
	})
	at := newTestActionType("adminAction", []ParameterDef{
		{ID: "role", Type: "string", Required: true},
	}, []Rule{
		{Type: "createObject", ObjectType: "Log"},
	})
	at.SubmissionCriteria = criteria

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "adminAction",
		Parameters: map[string]interface{}{"role": "viewer"},
	})
	if err == nil {
		t.Fatal("expected submission criteria error, got nil")
	}
}

// ---------------------------------------------------------------------------
// Side Effects Tests (40–44)
// ---------------------------------------------------------------------------

func TestExecuteSideEffects_Nil(t *testing.T) {
	if err := ExecuteSideEffects(nil, ActionResult{ActionRID: "rid-1"}); err != nil {
		t.Fatalf("expected nil for nil effects, got: %v", err)
	}
}

func TestExecuteSideEffects_Empty(t *testing.T) {
	if err := ExecuteSideEffects(json.RawMessage("[]"), ActionResult{ActionRID: "rid-1"}); err != nil {
		t.Fatalf("expected nil for empty effects, got: %v", err)
	}
}

func TestExecuteSideEffects_Log(t *testing.T) {
	effects := mustJSON([]SideEffect{
		{Type: "log", Config: mustJSON(map[string]interface{}{})},
	})
	result := ActionResult{ActionRID: "rid-log", BatchID: "batch-1"}
	// log effect should succeed without error
	if err := ExecuteSideEffects(effects, result); err != nil {
		t.Fatalf("expected nil for log effect, got: %v", err)
	}
}

func TestExecuteSideEffects_Webhook_Success(t *testing.T) {
	// Start a test HTTP server to receive the webhook.
	var received []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		received = buf.Bytes()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	effects := mustJSON([]SideEffect{
		{
			Type: "webhook",
			Config: mustJSON(webhookConfig{
				URL: srv.URL,
			}),
		},
	})
	result := ActionResult{ActionRID: "rid-wh", BatchID: "batch-wh"}
	if err := ExecuteSideEffects(effects, result); err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}
	if len(received) == 0 {
		t.Fatal("expected webhook body to be non-empty")
	}
	var got ActionResult
	if err := json.Unmarshal(received, &got); err != nil {
		t.Fatalf("unmarshal webhook body: %v", err)
	}
	if got.ActionRID != "rid-wh" {
		t.Fatalf("expected actionRid=rid-wh, got %s", got.ActionRID)
	}
}

func TestExecuteSideEffects_Webhook_NonSuccess_BestEffort(t *testing.T) {
	// Server returns 500 — side effect should log but not propagate error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	effects := mustJSON([]SideEffect{
		{
			Type: "webhook",
			Config: mustJSON(webhookConfig{
				URL: srv.URL,
			}),
		},
	})
	// ExecuteSideEffects swallows the error (best-effort).
	if err := ExecuteSideEffects(effects, ActionResult{ActionRID: "rid-fail"}); err != nil {
		t.Fatalf("expected nil (best-effort), got: %v", err)
	}
}

func (m *mockOmsRepo) CreateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error {
	return nil
}
func (m *mockOmsRepo) GetSecurityPolicy(_ context.Context, _ string) (*oms.SecurityPolicy, error) {
	return nil, nil
}
func (m *mockOmsRepo) ListSecurityPolicies(_ context.Context, _ string) ([]oms.SecurityPolicy, error) {
	return nil, nil
}
func (m *mockOmsRepo) UpdateSecurityPolicy(_ context.Context, _ *oms.SecurityPolicy) error {
	return nil
}
func (m *mockOmsRepo) DeleteSecurityPolicy(_ context.Context, _ string) error { return nil }

// ---------------------------------------------------------------------------
// Function-backed Action Tests (Tier 3.2)
// ---------------------------------------------------------------------------

// mockFunctionDispatcher records calls and returns a configurable result so
// the executor branch can be exercised without standing up an HTTP server.
type mockFunctionDispatcher struct {
	calls       int
	lastAT      *oms.ActionType
	lastParams  map[string]interface{}
	returnEdits []funnel.Edit
	returnErr   error
}

func (m *mockFunctionDispatcher) Dispatch(_ context.Context, at *oms.ActionType, params map[string]interface{}) ([]funnel.Edit, error) {
	m.calls++
	m.lastAT = at
	m.lastParams = params
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return m.returnEdits, nil
}

// TestExecutor_FunctionBacked_Dispatches verifies that when an ActionType is
// marked function-backed, the executor delegates rule evaluation to the
// dispatcher and uses the returned edits instead of running local rules.
func TestExecutor_FunctionBacked_Dispatches(t *testing.T) {
	at := newTestActionType("createEmployeeFn", []ParameterDef{
		{ID: "name", Type: "string", Required: true},
	}, []Rule{
		// Rules array is intentionally non-empty: the test asserts that
		// the dispatcher's edits are used INSTEAD of the rules' edits.
		{Type: "createObject", ObjectType: "ShouldNotAppear"},
	})
	at.IsFunctionBacked = true
	at.FunctionRID = "ri.functions.main.function.create-employee-fn"

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)

	disp := &mockFunctionDispatcher{
		returnEdits: []funnel.Edit{{
			Type:       funnel.EditTypeCreate,
			ObjectType: "Employee",
			PrimaryKey: "emp-from-fn",
			Properties: map[string]interface{}{"name": "Alice"},
		}},
	}
	exec.SetFunctionDispatcher(disp)

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployeeFn",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if disp.calls != 1 {
		t.Fatalf("expected dispatcher called once, got %d", disp.calls)
	}
	if disp.lastAT == nil || disp.lastAT.FunctionRID != at.FunctionRID {
		t.Errorf("dispatcher received wrong action type: %+v", disp.lastAT)
	}
	if disp.lastParams["name"] != "Alice" {
		t.Errorf("dispatcher received wrong params: %+v", disp.lastParams)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit from dispatcher, got %d", len(result.Edits))
	}
	if result.Edits[0].PrimaryKey != "emp-from-fn" {
		t.Errorf("expected dispatcher edit to win, got pk %q", result.Edits[0].PrimaryKey)
	}
	if result.Edits[0].ObjectType != "Employee" {
		t.Errorf("expected dispatcher's Employee, got %q", result.Edits[0].ObjectType)
	}
}

// TestExecutor_FunctionBacked_NoDispatcher_FallsBackToRules verifies that an
// IsFunctionBacked=true action with no configured dispatcher still runs local
// rules — preserving graceful degradation for dev environments where the
// function service is not running.
func TestExecutor_FunctionBacked_NoDispatcher_FallsBackToRules(t *testing.T) {
	at := newTestActionType("createEmployeeFn", []ParameterDef{
		{ID: "name", Type: "string", Required: true},
	}, []Rule{
		{
			Type:       "createObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
	})
	at.IsFunctionBacked = true
	at.FunctionRID = "ri.functions.main.function.unconfigured"

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	// No SetFunctionDispatcher: nil dispatcher path.

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployeeFn",
		Parameters: map[string]interface{}{"name": "Bob"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 rule-derived edit, got %d", len(result.Edits))
	}
	if result.Edits[0].ObjectType != "Employee" {
		t.Errorf("expected rule's Employee, got %q", result.Edits[0].ObjectType)
	}
	if result.Edits[0].Properties["name"] != "Bob" {
		t.Errorf("expected name=Bob from rules, got %v", result.Edits[0].Properties["name"])
	}
}

// TestExecutor_FunctionBacked_DispatcherError_Propagates verifies that a
// failing dispatcher fails the action so the caller sees the function error.
func TestExecutor_FunctionBacked_DispatcherError_Propagates(t *testing.T) {
	at := newTestActionType("badFn", nil, nil)
	at.IsFunctionBacked = true
	at.FunctionRID = "ri.functions.main.function.bad"

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)
	exec.SetFunctionDispatcher(&mockFunctionDispatcher{
		returnErr: fmt.Errorf("boom"),
	})

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "badFn",
		Parameters: map[string]interface{}{},
	})
	if err == nil {
		t.Fatal("expected dispatcher error to propagate")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected error to mention 'boom', got %v", err)
	}
}

// TestExecutor_NotFunctionBacked_DispatcherIgnored verifies an action type
// with IsFunctionBacked=false never reaches the dispatcher, even when one is
// configured. This protects regular actions from accidental redirection.
func TestExecutor_NotFunctionBacked_DispatcherIgnored(t *testing.T) {
	at := newTestActionType("createEmployee", []ParameterDef{
		{ID: "name", Type: "string", Required: true},
	}, []Rule{
		{
			Type:       "createObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
	})
	at.IsFunctionBacked = false

	repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
	exec := NewExecutor(repo, nil)

	disp := &mockFunctionDispatcher{
		returnEdits: []funnel.Edit{{
			Type:       funnel.EditTypeCreate,
			ObjectType: "ShouldNotBeUsed",
			PrimaryKey: "x",
		}},
	}
	exec.SetFunctionDispatcher(disp)

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if disp.calls != 0 {
		t.Errorf("expected dispatcher NOT called, got %d calls", disp.calls)
	}
	if len(result.Edits) != 1 || result.Edits[0].ObjectType != "Employee" {
		t.Errorf("expected rules path Employee edit, got %+v", result.Edits)
	}
}

// ---------------------------------------------------------------------------
// createLink Rule Tests (US-100)
// ---------------------------------------------------------------------------

func TestParseRules_CreateLink(t *testing.T) {
	data := mustJSON([]Rule{
		{
			Type:                   "createLink",
			LinkTypeAPIName:        "employeeDepartment",
			SourceObjectPrimaryKey: "employeeId",
			TargetObjectPrimaryKey: "departmentId",
		},
	})
	rules, err := ParseRules(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 || rules[0].Type != "createLink" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
	if rules[0].LinkTypeAPIName != "employeeDepartment" {
		t.Fatalf("expected LinkTypeAPIName employeeDepartment, got %s", rules[0].LinkTypeAPIName)
	}
}

func TestExecuteRules_CreateLink(t *testing.T) {
	rules := []Rule{
		{
			Type:                   "createLink",
			LinkTypeAPIName:        "employeeDepartment",
			SourceObjectPrimaryKey: "employeeId",
			TargetObjectPrimaryKey: "departmentId",
		},
	}
	params := map[string]interface{}{
		"employeeId":   "emp-1",
		"departmentId": "dept-1",
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeLinkCreate {
		t.Fatalf("expected LINK_CREATE, got %s", edits[0].Type)
	}
	if edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected source PK emp-1, got %s", edits[0].PrimaryKey)
	}
	if edits[0].TargetPrimaryKey != "dept-1" {
		t.Fatalf("expected target PK dept-1, got %s", edits[0].TargetPrimaryKey)
	}
	if edits[0].LinkTypeRID != "employeeDepartment" {
		t.Fatalf("expected LinkTypeRID employeeDepartment (api name, pre-resolve), got %s", edits[0].LinkTypeRID)
	}
}

func TestExecuteRules_CreateLink_MissingSourcePK(t *testing.T) {
	rules := []Rule{
		{
			Type:                   "createLink",
			LinkTypeAPIName:        "employeeDepartment",
			SourceObjectPrimaryKey: "employeeId",
			TargetObjectPrimaryKey: "departmentId",
		},
	}
	params := map[string]interface{}{
		"departmentId": "dept-1",
	}
	_, err := ExecuteRules(rules, params)
	if err == nil {
		t.Fatal("expected error for missing source PK")
	}
}

func TestExecuteRules_CreateLink_MissingTargetPK(t *testing.T) {
	rules := []Rule{
		{
			Type:                   "createLink",
			LinkTypeAPIName:        "employeeDepartment",
			SourceObjectPrimaryKey: "employeeId",
			TargetObjectPrimaryKey: "departmentId",
		},
	}
	params := map[string]interface{}{
		"employeeId": "emp-1",
	}
	_, err := ExecuteRules(rules, params)
	if err == nil {
		t.Fatal("expected error for missing target PK")
	}
}

func TestExecuteRules_CreateLink_WithObjectRules(t *testing.T) {
	rules := []Rule{
		{
			Type:       "createObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
		{
			Type:                   "createLink",
			LinkTypeAPIName:        "employeeDepartment",
			SourceObjectPrimaryKey: "employeeId",
			TargetObjectPrimaryKey: "departmentId",
		},
	}
	params := map[string]interface{}{
		"name":         "Alice",
		"employeeId":   "emp-1",
		"departmentId": "dept-1",
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected first edit CREATE, got %s", edits[0].Type)
	}
	if edits[1].Type != funnel.EditTypeLinkCreate {
		t.Fatalf("expected second edit LINK_CREATE, got %s", edits[1].Type)
	}
}

// ---------------------------------------------------------------------------
// CollapseEdits LINK_CREATE Tests (US-100)
// ---------------------------------------------------------------------------

func TestCollapseEdits_LinkCreate_NoDuplicate(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeLinkCreate, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
		{Type: funnel.EditTypeLinkCreate, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
	}
	result := CollapseEdits(edits)
	if len(result) != 1 {
		t.Fatalf("expected 1 link edit (deduped), got %d", len(result))
	}
	if result[0].Type != funnel.EditTypeLinkCreate {
		t.Fatalf("expected LINK_CREATE, got %s", result[0].Type)
	}
}

func TestCollapseEdits_LinkCreate_DifferentTargets(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeLinkCreate, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
		{Type: funnel.EditTypeLinkCreate, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-2"},
	}
	result := CollapseEdits(edits)
	if len(result) != 2 {
		t.Fatalf("expected 2 link edits (different targets), got %d", len(result))
	}
}

func TestCollapseEdits_MixedObjectAndLink(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Employee", PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice"}},
		{Type: funnel.EditTypeLinkCreate, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
	}
	result := CollapseEdits(edits)
	if len(result) != 2 {
		t.Fatalf("expected 2 edits (1 object + 1 link), got %d", len(result))
	}
	if result[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected first edit CREATE, got %s", result[0].Type)
	}
	if result[1].Type != funnel.EditTypeLinkCreate {
		t.Fatalf("expected second edit LINK_CREATE, got %s", result[1].Type)
	}
}

// ---------------------------------------------------------------------------
// Executor createLink Tests (US-100)
// ---------------------------------------------------------------------------

func TestExecutor_Apply_CreateLink(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("linkEmployeeDept", []ParameterDef{
				{ID: "employeeId", Type: "string", Required: true},
				{ID: "departmentId", Type: "string", Required: true},
			}, []Rule{
				{
					Type:                   "createLink",
					LinkTypeAPIName:        "employeeDepartment",
					SourceObjectPrimaryKey: "employeeId",
					TargetObjectPrimaryKey: "departmentId",
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "linkEmployeeDept",
		Parameters: map[string]interface{}{
			"employeeId":   "emp-1",
			"departmentId": "dept-1",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeLinkCreate {
		t.Fatalf("expected LINK_CREATE, got %s", result.Edits[0].Type)
	}
	if result.Edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected source PK emp-1, got %s", result.Edits[0].PrimaryKey)
	}
	if result.Edits[0].TargetPrimaryKey != "dept-1" {
		t.Fatalf("expected target PK dept-1, got %s", result.Edits[0].TargetPrimaryKey)
	}
}

func TestCountEdits_IncludesLinks(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1"},
		{Type: funnel.EditTypeLinkCreate, PrimaryKey: "1", LinkTypeRID: "lt-1", TargetPrimaryKey: "2"},
	}
	r := countEdits(edits)
	if r.AddedObjectCount != 1 {
		t.Fatalf("expected addedObjectCount=1, got %d", r.AddedObjectCount)
	}
	if r.AddedLinksCount != 1 {
		t.Fatalf("expected addedLinksCount=1, got %d", r.AddedLinksCount)
	}
}

// ---------------------------------------------------------------------------
// deleteLink Rule Tests (US-101)
// ---------------------------------------------------------------------------

func TestParseRules_DeleteLink(t *testing.T) {
	data := mustJSON([]Rule{
		{
			Type:                   "deleteLink",
			LinkTypeAPIName:        "employeeDepartment",
			SourceObjectPrimaryKey: "employeeId",
			TargetObjectPrimaryKey: "departmentId",
		},
	})
	rules, err := ParseRules(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 || rules[0].Type != "deleteLink" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
	if rules[0].LinkTypeAPIName != "employeeDepartment" {
		t.Fatalf("expected LinkTypeAPIName employeeDepartment, got %s", rules[0].LinkTypeAPIName)
	}
}

func TestExecuteRules_DeleteLink(t *testing.T) {
	rules := []Rule{
		{
			Type:                   "deleteLink",
			LinkTypeAPIName:        "employeeDepartment",
			SourceObjectPrimaryKey: "employeeId",
			TargetObjectPrimaryKey: "departmentId",
		},
	}
	params := map[string]interface{}{
		"employeeId":   "emp-1",
		"departmentId": "dept-1",
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeLinkDelete {
		t.Fatalf("expected LINK_DELETE, got %s", edits[0].Type)
	}
	if edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected source PK emp-1, got %s", edits[0].PrimaryKey)
	}
	if edits[0].TargetPrimaryKey != "dept-1" {
		t.Fatalf("expected target PK dept-1, got %s", edits[0].TargetPrimaryKey)
	}
	if edits[0].LinkTypeRID != "employeeDepartment" {
		t.Fatalf("expected LinkTypeRID employeeDepartment (api name, pre-resolve), got %s", edits[0].LinkTypeRID)
	}
}

func TestExecuteRules_DeleteLink_MissingSourcePK(t *testing.T) {
	rules := []Rule{
		{
			Type:                   "deleteLink",
			LinkTypeAPIName:        "employeeDepartment",
			SourceObjectPrimaryKey: "employeeId",
			TargetObjectPrimaryKey: "departmentId",
		},
	}
	params := map[string]interface{}{
		"departmentId": "dept-1",
	}
	_, err := ExecuteRules(rules, params)
	if err == nil {
		t.Fatal("expected error for missing source PK")
	}
}

func TestExecuteRules_DeleteLink_MissingTargetPK(t *testing.T) {
	rules := []Rule{
		{
			Type:                   "deleteLink",
			LinkTypeAPIName:        "employeeDepartment",
			SourceObjectPrimaryKey: "employeeId",
			TargetObjectPrimaryKey: "departmentId",
		},
	}
	params := map[string]interface{}{
		"employeeId": "emp-1",
	}
	_, err := ExecuteRules(rules, params)
	if err == nil {
		t.Fatal("expected error for missing target PK")
	}
}

// ---------------------------------------------------------------------------
// CollapseEdits LINK_DELETE Tests (US-101)
// ---------------------------------------------------------------------------

func TestCollapseEdits_LinkCreateThenDelete_Cancels(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeLinkCreate, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
		{Type: funnel.EditTypeLinkDelete, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
	}
	result := CollapseEdits(edits)
	if len(result) != 0 {
		t.Fatalf("expected 0 edits (LINK_CREATE+LINK_DELETE cancel), got %d", len(result))
	}
}

func TestCollapseEdits_LinkDelete_NoDuplicate(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeLinkDelete, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
		{Type: funnel.EditTypeLinkDelete, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
	}
	result := CollapseEdits(edits)
	if len(result) != 1 {
		t.Fatalf("expected 1 link delete (deduped), got %d", len(result))
	}
	if result[0].Type != funnel.EditTypeLinkDelete {
		t.Fatalf("expected LINK_DELETE, got %s", result[0].Type)
	}
}

func TestCollapseEdits_LinkDelete_DifferentTargets(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeLinkDelete, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
		{Type: funnel.EditTypeLinkDelete, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-2"},
	}
	result := CollapseEdits(edits)
	if len(result) != 2 {
		t.Fatalf("expected 2 link deletes (different targets), got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Executor deleteLink Tests (US-101)
// ---------------------------------------------------------------------------

func TestExecutor_Apply_DeleteLink(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("unlinkEmployeeDept", []ParameterDef{
				{ID: "employeeId", Type: "string", Required: true},
				{ID: "departmentId", Type: "string", Required: true},
			}, []Rule{
				{
					Type:                   "deleteLink",
					LinkTypeAPIName:        "employeeDepartment",
					SourceObjectPrimaryKey: "employeeId",
					TargetObjectPrimaryKey: "departmentId",
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "unlinkEmployeeDept",
		Parameters: map[string]interface{}{
			"employeeId":   "emp-1",
			"departmentId": "dept-1",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeLinkDelete {
		t.Fatalf("expected LINK_DELETE, got %s", result.Edits[0].Type)
	}
	if result.Edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected source PK emp-1, got %s", result.Edits[0].PrimaryKey)
	}
	if result.Edits[0].TargetPrimaryKey != "dept-1" {
		t.Fatalf("expected target PK dept-1, got %s", result.Edits[0].TargetPrimaryKey)
	}
}

func TestCountEdits_IncludesDeletedLinks(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeDelete, ObjectType: "A", PrimaryKey: "1"},
		{Type: funnel.EditTypeLinkDelete, PrimaryKey: "1", LinkTypeRID: "lt-1", TargetPrimaryKey: "2"},
	}
	r := countEdits(edits)
	if r.DeletedObjectCount != 1 {
		t.Fatalf("expected deletedObjectCount=1, got %d", r.DeletedObjectCount)
	}
	if r.DeletedLinksCount != 1 {
		t.Fatalf("expected deletedLinksCount=1, got %d", r.DeletedLinksCount)
	}
}

// ---------------------------------------------------------------------------
// createOrModifyObject Rule Tests (US-102)
// ---------------------------------------------------------------------------

func TestParseRules_CreateOrModifyObject(t *testing.T) {
	data := mustJSON([]Rule{
		{
			Type:       "createOrModifyObject",
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
	if len(rules) != 1 || rules[0].Type != "createOrModifyObject" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
}

func TestExecuteRules_CreateOrModifyObject_WithPK(t *testing.T) {
	rules := []Rule{
		{
			Type:       "createOrModifyObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
	}
	params := map[string]interface{}{
		"primaryKey": "emp-1",
		"name":       "Alice",
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != editTypeUpsert {
		t.Fatalf("expected UPSERT, got %s", edits[0].Type)
	}
	if edits[0].ObjectType != "Employee" {
		t.Fatalf("expected Employee, got %s", edits[0].ObjectType)
	}
	if edits[0].PrimaryKey != "emp-1" {
		t.Fatalf("expected primaryKey emp-1, got %s", edits[0].PrimaryKey)
	}
	if edits[0].Properties["name"] != "Alice" {
		t.Fatalf("expected name=Alice, got %v", edits[0].Properties["name"])
	}
}

func TestExecuteRules_CreateOrModifyObject_NoPK(t *testing.T) {
	rules := []Rule{
		{
			Type:       "createOrModifyObject",
			ObjectType: "Employee",
			PropertyBindings: map[string]PropertyBinding{
				"name": {Type: "parameter", Value: "name"},
			},
		},
	}
	params := map[string]interface{}{
		"name": "Alice",
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	// No PK in params → auto-generate, always create
	if edits[0].Type != editTypeUpsert {
		t.Fatalf("expected UPSERT, got %s", edits[0].Type)
	}
	if edits[0].PrimaryKey == "" {
		t.Fatal("expected auto-generated primary key, got empty")
	}
}

// ---------------------------------------------------------------------------
// Executor createOrModifyObject Tests (US-102)
// ---------------------------------------------------------------------------

// mockObjectExistenceChecker allows tests to control which objects "exist".
type mockObjectExistenceChecker struct {
	existing map[string]bool // key: "objectType|primaryKey"
}

func (m *mockObjectExistenceChecker) ObjectExists(_ context.Context, objectType, primaryKey string) bool {
	if m.existing == nil {
		return false
	}
	return m.existing[objectType+"|"+primaryKey]
}

func TestExecutor_Apply_CreateOrModify_ObjectNotExists(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("upsertEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createOrModifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	exec.SetObjectExistenceChecker(&mockObjectExistenceChecker{
		existing: map[string]bool{}, // empty → nothing exists
	})

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "upsertEmployee",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-new",
			"name":       "Alice",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected CREATE (object not exists), got %s", result.Edits[0].Type)
	}
	if result.Edits[0].PrimaryKey != "emp-new" {
		t.Fatalf("expected primaryKey emp-new, got %s", result.Edits[0].PrimaryKey)
	}
	if result.Edits[0].Properties["name"] != "Alice" {
		t.Fatalf("expected name=Alice, got %v", result.Edits[0].Properties["name"])
	}
}

func TestExecutor_Apply_CreateOrModify_ObjectExists(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("upsertEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createOrModifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	exec.SetObjectExistenceChecker(&mockObjectExistenceChecker{
		existing: map[string]bool{
			"Employee|emp-existing": true,
		},
	})

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "upsertEmployee",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-existing",
			"name":       "Bob Updated",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeModify {
		t.Fatalf("expected MODIFY (object exists), got %s", result.Edits[0].Type)
	}
	if result.Edits[0].PrimaryKey != "emp-existing" {
		t.Fatalf("expected primaryKey emp-existing, got %s", result.Edits[0].PrimaryKey)
	}
	if result.Edits[0].Properties["name"] != "Bob Updated" {
		t.Fatalf("expected name='Bob Updated', got %v", result.Edits[0].Properties["name"])
	}
}

func TestExecutor_Apply_CreateOrModify_NoChecker_DefaultsToCreate(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("upsertEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createOrModifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	// No SetObjectExistenceChecker → nil checker → defaults to CREATE

	result, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "upsertEmployee",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-1",
			"name":       "Alice",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected CREATE (no checker → default), got %s", result.Edits[0].Type)
	}
}

// ---------------------------------------------------------------------------
// CollapseEdits with resolved createOrModifyObject Tests (US-102)
// ---------------------------------------------------------------------------

func TestCollapseEdits_UpsertResolvedToCreate_ThenModify(t *testing.T) {
	// Simulates: createOrModifyObject resolved to CREATE, followed by modifyObject
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"y": 2}},
	}
	result := CollapseEdits(edits)
	if len(result) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result))
	}
	if result[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected CREATE with merged props, got %s", result[0].Type)
	}
	if result[0].Properties["x"] != 1 || result[0].Properties["y"] != 2 {
		t.Fatalf("expected merged {x:1, y:2}, got %v", result[0].Properties)
	}
}

func TestCollapseEdits_UpsertResolvedToModify_ThenDelete(t *testing.T) {
	// Simulates: createOrModifyObject resolved to MODIFY, followed by deleteObject
	edits := []funnel.Edit{
		{Type: funnel.EditTypeModify, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeDelete, ObjectType: "A", PrimaryKey: "1"},
	}
	result := CollapseEdits(edits)
	if len(result) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result))
	}
	if result[0].Type != funnel.EditTypeDelete {
		t.Fatalf("expected DELETE (MODIFY+DELETE→DELETE), got %s", result[0].Type)
	}
}

func TestCollapseEdits_UpsertResolvedToCreate_ThenDelete(t *testing.T) {
	// Simulates: createOrModifyObject resolved to CREATE, followed by deleteObject
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "A", PrimaryKey: "1", Properties: map[string]interface{}{"x": 1}},
		{Type: funnel.EditTypeDelete, ObjectType: "A", PrimaryKey: "1"},
	}
	result := CollapseEdits(edits)
	if len(result) != 0 {
		t.Fatalf("expected 0 edits (CREATE+DELETE cancel), got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// Interface-backed Action Rules (US-103)
// ---------------------------------------------------------------------------

func TestParseRules_InterfaceRuleTypes(t *testing.T) {
	raw := mustJSON([]Rule{
		{Type: "createInterfaceObject", InterfaceAPIName: "GeoEntity"},
		{Type: "modifyInterfaceObject", InterfaceAPIName: "GeoEntity"},
		{Type: "deleteInterfaceObject", InterfaceAPIName: "GeoEntity"},
	})
	rules, err := ParseRules(raw)
	if err != nil {
		t.Fatalf("ParseRules: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(rules))
	}
	for i, expected := range []string{"createInterfaceObject", "modifyInterfaceObject", "deleteInterfaceObject"} {
		if rules[i].Type != expected {
			t.Fatalf("rule[%d]: expected type %q, got %q", i, expected, rules[i].Type)
		}
		if rules[i].InterfaceAPIName != "GeoEntity" {
			t.Fatalf("rule[%d]: expected interfaceApiName GeoEntity, got %q", i, rules[i].InterfaceAPIName)
		}
	}
}

func TestExecuteRule_CreateInterfaceObject(t *testing.T) {
	rule := Rule{
		Type:             "createInterfaceObject",
		InterfaceAPIName: "GeoEntity",
		PropertyBindings: map[string]PropertyBinding{
			"name": {Type: "parameter", Value: "name"},
		},
	}
	params := map[string]interface{}{
		"objectType": "Building",
		"name":       "HQ",
	}
	edits, err := ExecuteRules([]Rule{rule}, params)
	if err != nil {
		t.Fatalf("ExecuteRules: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected CREATE, got %s", edits[0].Type)
	}
	if edits[0].ObjectType != "Building" {
		t.Fatalf("expected objectType=Building, got %s", edits[0].ObjectType)
	}
	if edits[0].Properties["name"] != "HQ" {
		t.Fatalf("expected name=HQ, got %v", edits[0].Properties["name"])
	}
}

func TestExecuteRule_ModifyInterfaceObject(t *testing.T) {
	rule := Rule{
		Type:             "modifyInterfaceObject",
		InterfaceAPIName: "GeoEntity",
		PropertyBindings: map[string]PropertyBinding{
			"name": {Type: "parameter", Value: "name"},
		},
	}
	params := map[string]interface{}{
		"objectType": "Building",
		"primaryKey": "bld-1",
		"name":       "HQ Updated",
	}
	edits, err := ExecuteRules([]Rule{rule}, params)
	if err != nil {
		t.Fatalf("ExecuteRules: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeModify {
		t.Fatalf("expected MODIFY, got %s", edits[0].Type)
	}
	if edits[0].ObjectType != "Building" {
		t.Fatalf("expected objectType=Building, got %s", edits[0].ObjectType)
	}
	if edits[0].PrimaryKey != "bld-1" {
		t.Fatalf("expected primaryKey=bld-1, got %s", edits[0].PrimaryKey)
	}
}

func TestExecuteRule_DeleteInterfaceObject(t *testing.T) {
	rule := Rule{
		Type:             "deleteInterfaceObject",
		InterfaceAPIName: "GeoEntity",
	}
	params := map[string]interface{}{
		"objectType": "Building",
		"primaryKey": "bld-1",
	}
	edits, err := ExecuteRules([]Rule{rule}, params)
	if err != nil {
		t.Fatalf("ExecuteRules: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
	if edits[0].Type != funnel.EditTypeDelete {
		t.Fatalf("expected DELETE, got %s", edits[0].Type)
	}
	if edits[0].ObjectType != "Building" {
		t.Fatalf("expected objectType=Building, got %s", edits[0].ObjectType)
	}
}

func TestExecuteRule_InterfaceObject_MissingObjectType(t *testing.T) {
	for _, ruleType := range []string{"createInterfaceObject", "modifyInterfaceObject", "deleteInterfaceObject"} {
		t.Run(ruleType, func(t *testing.T) {
			rule := Rule{Type: ruleType, InterfaceAPIName: "GeoEntity"}
			// No objectType in params
			_, err := ExecuteRules([]Rule{rule}, map[string]interface{}{"name": "test"})
			if err == nil {
				t.Fatal("expected error for missing objectType, got nil")
			}
		})
	}
}

func TestExecutor_InterfaceRule_ValidatesInterfaceMembership(t *testing.T) {
	repo := &interfaceAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createGeoEntity", []ParameterDef{
					{ID: "objectType", Type: "string", Required: true},
					{ID: "name", Type: "string", Required: true},
				}, []Rule{
					{
						Type:             "createInterfaceObject",
						InterfaceAPIName: "GeoEntity",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						},
					},
				}),
			},
		},
		interfaces: map[string]*oms.Interface{
			"GeoEntity": {
				RID:     "ri.ontology.main.interface.geo-entity",
				APIName: "GeoEntity",
			},
		},
		objectTypesByName: map[string]*oms.ObjectType{
			"Building": {
				RID:     "ri.ontology.main.object-type.Building",
				APIName: "Building",
			},
		},
		// Building implements GeoEntity
		objectTypeInterfaces: map[string][]oms.ObjectTypeInterface{
			"ri.ontology.main.object-type.Building": {
				{ObjectTypeRID: "ri.ontology.main.object-type.Building", InterfaceRID: "ri.ontology.main.interface.geo-entity"},
			},
		},
	}

	exec := NewExecutor(repo, nil)
	result, err := exec.Apply(context.Background(), "test-ont", &ApplyRequest{
		ActionType: "createGeoEntity",
		Parameters: map[string]interface{}{
			"objectType": "Building",
			"name":       "HQ",
		},
	})
	if err != nil {
		t.Fatalf("Apply should succeed for implementing type: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(result.Edits))
	}
	if result.Edits[0].ObjectType != "Building" {
		t.Fatalf("expected objectType=Building, got %s", result.Edits[0].ObjectType)
	}
}

func TestExecutor_InterfaceRule_RejectsNonImplementingType(t *testing.T) {
	repo := &interfaceAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createGeoEntity", []ParameterDef{
					{ID: "objectType", Type: "string", Required: true},
					{ID: "name", Type: "string", Required: true},
				}, []Rule{
					{
						Type:             "createInterfaceObject",
						InterfaceAPIName: "GeoEntity",
						PropertyBindings: map[string]PropertyBinding{
							"name": {Type: "parameter", Value: "name"},
						},
					},
				}),
			},
		},
		interfaces: map[string]*oms.Interface{
			"GeoEntity": {
				RID:     "ri.ontology.main.interface.geo-entity",
				APIName: "GeoEntity",
			},
		},
		objectTypesByName: map[string]*oms.ObjectType{
			"Vehicle": {
				RID:     "ri.ontology.main.object-type.Vehicle",
				APIName: "Vehicle",
			},
		},
		// Vehicle does NOT implement GeoEntity
		objectTypeInterfaces: map[string][]oms.ObjectTypeInterface{},
	}

	exec := NewExecutor(repo, nil)
	_, err := exec.Apply(context.Background(), "test-ont", &ApplyRequest{
		ActionType: "createGeoEntity",
		Parameters: map[string]interface{}{
			"objectType": "Vehicle",
			"name":       "Car",
		},
	})
	if err == nil {
		t.Fatal("expected error for non-implementing type, got nil")
	}
	if !strings.Contains(err.Error(), "does not implement interface") {
		t.Fatalf("expected 'does not implement interface' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// PrevEdits / Undo Tests (US-104)
// ---------------------------------------------------------------------------

// mockObjectFetcher returns pre-configured object states for testing PrevEdits.
type mockObjectFetcher struct {
	objects map[string]map[string]interface{} // "objectType|primaryKey" → properties
}

func (m *mockObjectFetcher) FetchObject(_ context.Context, _, objectType, primaryKey string) (map[string]interface{}, error) {
	key := objectType + "|" + primaryKey
	if props, ok := m.objects[key]; ok {
		return props, nil
	}
	return nil, nil
}

func TestExecutor_PrevEdits_CreateHasNullEntry(t *testing.T) {
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
	exec.SetObjectFetcher(&mockObjectFetcher{objects: map[string]map[string]interface{}{}})

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{"name": "Alice"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
	log := repo.insertedLogs[0]
	if log.PrevEdits == nil {
		t.Fatal("expected PrevEdits to be non-nil (should be JSON array with null entry)")
	}

	var prevEdits []json.RawMessage
	if err := json.Unmarshal(log.PrevEdits, &prevEdits); err != nil {
		t.Fatalf("failed to unmarshal PrevEdits: %v", err)
	}
	if len(prevEdits) != 1 {
		t.Fatalf("expected 1 PrevEdits entry, got %d", len(prevEdits))
	}
	// CREATE edits should have null PrevEdits entry
	if string(prevEdits[0]) != "null" {
		t.Fatalf("expected null for CREATE PrevEdits entry, got %s", prevEdits[0])
	}
}

func TestExecutor_PrevEdits_ModifyHasPreviousProperties(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("updateEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "modifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	fetcher := &mockObjectFetcher{
		objects: map[string]map[string]interface{}{
			"Employee|emp-1": {"name": "OldAlice", "age": float64(30)},
		},
	}
	exec := NewExecutor(repo, nil)
	exec.SetObjectFetcher(fetcher)

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "updateEmployee",
		Parameters: map[string]interface{}{"primaryKey": "emp-1", "name": "NewAlice"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
	log := repo.insertedLogs[0]
	if log.PrevEdits == nil {
		t.Fatal("expected PrevEdits to be non-nil for MODIFY action")
	}

	var prevEdits []json.RawMessage
	if err := json.Unmarshal(log.PrevEdits, &prevEdits); err != nil {
		t.Fatalf("failed to unmarshal PrevEdits: %v", err)
	}
	if len(prevEdits) != 1 {
		t.Fatalf("expected 1 PrevEdits entry, got %d", len(prevEdits))
	}

	var prevProps map[string]interface{}
	if err := json.Unmarshal(prevEdits[0], &prevProps); err != nil {
		t.Fatalf("failed to unmarshal MODIFY PrevEdits entry: %v", err)
	}
	if prevProps["name"] != "OldAlice" {
		t.Fatalf("expected previous name 'OldAlice', got %v", prevProps["name"])
	}
	if prevProps["age"] != float64(30) {
		t.Fatalf("expected previous age 30, got %v", prevProps["age"])
	}
}

func TestExecutor_PrevEdits_DeleteHasFullPreviousObject(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("deleteEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "deleteObject",
					ObjectType: "Employee",
				},
			}),
		},
	}
	fetcher := &mockObjectFetcher{
		objects: map[string]map[string]interface{}{
			"Employee|emp-1": {"name": "Alice", "age": float64(30), "dept": "Engineering"},
		},
	}
	exec := NewExecutor(repo, nil)
	exec.SetObjectFetcher(fetcher)

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "deleteEmployee",
		Parameters: map[string]interface{}{"primaryKey": "emp-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
	log := repo.insertedLogs[0]
	if log.PrevEdits == nil {
		t.Fatal("expected PrevEdits to be non-nil for DELETE action")
	}

	var prevEdits []json.RawMessage
	if err := json.Unmarshal(log.PrevEdits, &prevEdits); err != nil {
		t.Fatalf("failed to unmarshal PrevEdits: %v", err)
	}
	if len(prevEdits) != 1 {
		t.Fatalf("expected 1 PrevEdits entry, got %d", len(prevEdits))
	}

	var prevProps map[string]interface{}
	if err := json.Unmarshal(prevEdits[0], &prevProps); err != nil {
		t.Fatalf("failed to unmarshal DELETE PrevEdits entry: %v", err)
	}
	if prevProps["name"] != "Alice" {
		t.Fatalf("expected previous name 'Alice', got %v", prevProps["name"])
	}
	if prevProps["dept"] != "Engineering" {
		t.Fatalf("expected previous dept 'Engineering', got %v", prevProps["dept"])
	}
}

func TestExecutor_PrevEdits_NoFetcherPrevEditsNil(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("updateEmployee", []ParameterDef{
				{ID: "primaryKey", Type: "string", Required: true},
				{ID: "name", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "modifyObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			}),
		},
	}
	exec := NewExecutor(repo, nil)
	// No SetObjectFetcher call — graceful degradation

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "updateEmployee",
		Parameters: map[string]interface{}{"primaryKey": "emp-1", "name": "NewAlice"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
	log := repo.insertedLogs[0]
	// When no ObjectFetcher is set, PrevEdits should be nil (backward compatible)
	if log.PrevEdits != nil {
		t.Fatalf("expected nil PrevEdits when no fetcher is set, got %s", string(log.PrevEdits))
	}
}

func TestExecutor_PrevEdits_MixedEditsParallelSlice(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("complexAction", []ParameterDef{
				{ID: "newName", Type: "string", Required: true},
				{ID: "TaskId", Type: "string", Required: true},
				{ID: "ProjectId", Type: "string", Required: true},
			}, []Rule{
				{
					Type:       "createObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "newName"},
					},
				},
				{
					Type:       "modifyObject",
					ObjectType: "Task",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "static", Value: "Updated"},
					},
				},
				{
					Type:       "deleteObject",
					ObjectType: "Project",
				},
			}),
		},
	}
	fetcher := &mockObjectFetcher{
		objects: map[string]map[string]interface{}{
			"Task|task-1":    {"name": "BeforeUpdate"},
			"Project|proj-1": {"name": "BeforeDelete", "role": "Manager"},
		},
	}
	exec := NewExecutor(repo, nil)
	exec.SetObjectFetcher(fetcher)

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "complexAction",
		Parameters: map[string]interface{}{
			"newName":   "NewPerson",
			"TaskId":    "task-1",
			"ProjectId": "proj-1",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
	log := repo.insertedLogs[0]
	if log.PrevEdits == nil {
		t.Fatal("expected PrevEdits to be non-nil")
	}

	var prevEdits []json.RawMessage
	if err := json.Unmarshal(log.PrevEdits, &prevEdits); err != nil {
		t.Fatalf("failed to unmarshal PrevEdits: %v", err)
	}
	if len(prevEdits) != 3 {
		t.Fatalf("expected 3 PrevEdits entries (parallel to 3 edits), got %d", len(prevEdits))
	}

	// Entry 0: CREATE → null
	if string(prevEdits[0]) != "null" {
		t.Fatalf("expected null for CREATE entry, got %s", prevEdits[0])
	}

	// Entry 1: MODIFY Task → previous properties
	var modifyPrev map[string]interface{}
	if err := json.Unmarshal(prevEdits[1], &modifyPrev); err != nil {
		t.Fatalf("failed to unmarshal MODIFY PrevEdits: %v", err)
	}
	if modifyPrev["name"] != "BeforeUpdate" {
		t.Fatalf("expected MODIFY prev name 'BeforeUpdate', got %v", modifyPrev["name"])
	}

	// Entry 2: DELETE Project → full previous object
	var deletePrev map[string]interface{}
	if err := json.Unmarshal(prevEdits[2], &deletePrev); err != nil {
		t.Fatalf("failed to unmarshal DELETE PrevEdits: %v", err)
	}
	if deletePrev["name"] != "BeforeDelete" {
		t.Fatalf("expected DELETE prev name 'BeforeDelete', got %v", deletePrev["name"])
	}
	if deletePrev["role"] != "Manager" {
		t.Fatalf("expected DELETE prev role 'Manager', got %v", deletePrev["role"])
	}
}

func TestExecutor_PrevEdits_LinkEditsHaveNullEntry(t *testing.T) {
	repo := &mockOmsRepo{
		actionTypes: []oms.ActionType{
			newTestActionType("linkAction", []ParameterDef{
				{ID: "sourceId", Type: "string", Required: true},
				{ID: "targetId", Type: "string", Required: true},
			}, []Rule{
				{
					Type:                   "createLink",
					LinkTypeAPIName:        "employeeToProject",
					SourceObjectPrimaryKey: "sourceId",
					TargetObjectPrimaryKey: "targetId",
				},
			}),
		},
	}
	fetcher := &mockObjectFetcher{objects: map[string]map[string]interface{}{}}
	exec := NewExecutor(repo, nil)
	exec.SetObjectFetcher(fetcher)

	_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
		ActionType: "linkAction",
		Parameters: map[string]interface{}{"sourceId": "emp-1", "targetId": "proj-1"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.insertedLogs) != 1 {
		t.Fatalf("expected 1 action log, got %d", len(repo.insertedLogs))
	}
	log := repo.insertedLogs[0]
	if log.PrevEdits == nil {
		t.Fatal("expected PrevEdits to be non-nil for link action")
	}

	var prevEdits []json.RawMessage
	if err := json.Unmarshal(log.PrevEdits, &prevEdits); err != nil {
		t.Fatalf("failed to unmarshal PrevEdits: %v", err)
	}
	if len(prevEdits) != 1 {
		t.Fatalf("expected 1 PrevEdits entry, got %d", len(prevEdits))
	}
	// LINK_CREATE edits have null PrevEdits (like CREATE)
	if string(prevEdits[0]) != "null" {
		t.Fatalf("expected null for LINK_CREATE PrevEdits entry, got %s", prevEdits[0])
	}
}

// interfaceAwareMockRepo extends mockOmsRepo with interface resolution.
type interfaceAwareMockRepo struct {
	mockOmsRepo
	interfaces           map[string]*oms.Interface            // apiName → Interface
	objectTypesByName    map[string]*oms.ObjectType           // apiName → ObjectType
	objectTypeInterfaces map[string][]oms.ObjectTypeInterface // objectTypeRID → interfaces
}

func (m *interfaceAwareMockRepo) GetInterfaceByAPIName(_ context.Context, _, apiName string) (*oms.Interface, error) {
	if iface, ok := m.interfaces[apiName]; ok {
		return iface, nil
	}
	return nil, oms.ErrNotFound
}

func (m *interfaceAwareMockRepo) GetObjectTypeByAPIName(_ context.Context, _, apiName string) (*oms.ObjectType, error) {
	if ot, ok := m.objectTypesByName[apiName]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

func (m *interfaceAwareMockRepo) ListObjectTypeInterfaces(_ context.Context, objectTypeRID string) ([]oms.ObjectTypeInterface, error) {
	if otis, ok := m.objectTypeInterfaces[objectTypeRID]; ok {
		return otis, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Mock Publisher (shared by US-105+ tests)
// ---------------------------------------------------------------------------

type mockPublisher struct {
	callCount int
	lastBatch *funnel.EditBatch
	returnErr error
}

func (m *mockPublisher) Publish(batch *funnel.EditBatch) (uint64, error) {
	m.callCount++
	m.lastBatch = batch
	if m.returnErr != nil {
		return 0, m.returnErr
	}
	return uint64(m.callCount), nil
}

// ---------------------------------------------------------------------------
// Revert Action Tests (US-105)
// ---------------------------------------------------------------------------

func TestRevert_CREATE_GeneratesDeleteEdit(t *testing.T) {
	// A CREATE edit should be reverted with a DELETE edit.
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Employee", PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice"}},
	}
	editsJSON, _ := json.Marshal(edits)

	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			1: {
				ID:            1,
				ActionTypeRID: "ri.ontology.main.action-type.test",
				UserID:        "user-1",
				Parameters:    []byte(`{}`),
				Edits:         editsJSON,
				PrevEdits:     nil,
				Status:        "SUCCESS",
			},
		},
	}

	pub := &mockPublisher{}
	exec := NewExecutor(repo, pub)

	result, err := exec.Revert(context.Background(), "test-ontology", 1)
	if err != nil {
		t.Fatalf("Revert failed: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 reverse edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeDelete {
		t.Errorf("expected DELETE, got %s", result.Edits[0].Type)
	}
	if result.Edits[0].ObjectType != "Employee" {
		t.Errorf("expected Employee, got %s", result.Edits[0].ObjectType)
	}
	if result.Edits[0].PrimaryKey != "emp-1" {
		t.Errorf("expected emp-1, got %s", result.Edits[0].PrimaryKey)
	}
	// ActionLog should be marked as reverted.
	if repo.actionLogByID[1].Status != "REVERTED" {
		t.Errorf("expected status=REVERTED, got %s", repo.actionLogByID[1].Status)
	}
}

func TestRevert_MODIFY_RestoresPrevState(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeModify, ObjectType: "Employee", PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Bob"}},
	}
	prevEdits := []map[string]interface{}{
		{"name": "Alice", "age": float64(30)},
	}
	editsJSON, _ := json.Marshal(edits)
	prevEditsJSON, _ := json.Marshal(prevEdits)

	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			2: {
				ID:            2,
				ActionTypeRID: "ri.ontology.main.action-type.test",
				UserID:        "user-1",
				Parameters:    []byte(`{}`),
				Edits:         editsJSON,
				PrevEdits:     prevEditsJSON,
				Status:        "SUCCESS",
			},
		},
	}

	pub := &mockPublisher{}
	exec := NewExecutor(repo, pub)

	result, err := exec.Revert(context.Background(), "test-ontology", 2)
	if err != nil {
		t.Fatalf("Revert failed: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 reverse edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeModify {
		t.Errorf("expected MODIFY, got %s", result.Edits[0].Type)
	}
	if result.Edits[0].Properties["name"] != "Alice" {
		t.Errorf("expected name=Alice (prev state), got %v", result.Edits[0].Properties["name"])
	}
	if result.Edits[0].Properties["age"] != float64(30) {
		t.Errorf("expected age=30 (prev state), got %v", result.Edits[0].Properties["age"])
	}
}

func TestRevert_DELETE_RecreatesFromPrevState(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeDelete, ObjectType: "Employee", PrimaryKey: "emp-1"},
	}
	prevEdits := []map[string]interface{}{
		{"name": "Alice", "department": "Engineering"},
	}
	editsJSON, _ := json.Marshal(edits)
	prevEditsJSON, _ := json.Marshal(prevEdits)

	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			3: {
				ID:            3,
				ActionTypeRID: "ri.ontology.main.action-type.test",
				UserID:        "user-1",
				Parameters:    []byte(`{}`),
				Edits:         editsJSON,
				PrevEdits:     prevEditsJSON,
				Status:        "SUCCESS",
			},
		},
	}

	pub := &mockPublisher{}
	exec := NewExecutor(repo, pub)

	result, err := exec.Revert(context.Background(), "test-ontology", 3)
	if err != nil {
		t.Fatalf("Revert failed: %v", err)
	}
	if len(result.Edits) != 1 {
		t.Fatalf("expected 1 reverse edit, got %d", len(result.Edits))
	}
	if result.Edits[0].Type != funnel.EditTypeCreate {
		t.Errorf("expected CREATE, got %s", result.Edits[0].Type)
	}
	if result.Edits[0].Properties["name"] != "Alice" {
		t.Errorf("expected name=Alice, got %v", result.Edits[0].Properties["name"])
	}
	if result.Edits[0].Properties["department"] != "Engineering" {
		t.Errorf("expected department=Engineering, got %v", result.Edits[0].Properties["department"])
	}
}

func TestRevert_MixedEdits(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Employee", PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "New"}},
		{Type: funnel.EditTypeModify, ObjectType: "Employee", PrimaryKey: "emp-2", Properties: map[string]interface{}{"name": "Updated"}},
		{Type: funnel.EditTypeDelete, ObjectType: "Employee", PrimaryKey: "emp-3"},
	}
	prevEdits := []map[string]interface{}{
		nil,                                  // CREATE → no prev state
		{"name": "Original"},                 // MODIFY → prev properties
		{"name": "Deleted", "role": "admin"}, // DELETE → full prev object
	}
	editsJSON, _ := json.Marshal(edits)
	prevEditsJSON, _ := json.Marshal(prevEdits)

	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			4: {
				ID:        4,
				Edits:     editsJSON,
				PrevEdits: prevEditsJSON,
				Status:    "SUCCESS",
			},
		},
	}

	pub := &mockPublisher{}
	exec := NewExecutor(repo, pub)

	result, err := exec.Revert(context.Background(), "test-ontology", 4)
	if err != nil {
		t.Fatalf("Revert failed: %v", err)
	}
	if len(result.Edits) != 3 {
		t.Fatalf("expected 3 reverse edits, got %d", len(result.Edits))
	}
	// CREATE → DELETE
	if result.Edits[0].Type != funnel.EditTypeDelete {
		t.Errorf("edit[0]: expected DELETE, got %s", result.Edits[0].Type)
	}
	// MODIFY → MODIFY with prev state
	if result.Edits[1].Type != funnel.EditTypeModify {
		t.Errorf("edit[1]: expected MODIFY, got %s", result.Edits[1].Type)
	}
	if result.Edits[1].Properties["name"] != "Original" {
		t.Errorf("edit[1]: expected name=Original, got %v", result.Edits[1].Properties["name"])
	}
	// DELETE → CREATE with prev state
	if result.Edits[2].Type != funnel.EditTypeCreate {
		t.Errorf("edit[2]: expected CREATE, got %s", result.Edits[2].Type)
	}
	if result.Edits[2].Properties["role"] != "admin" {
		t.Errorf("edit[2]: expected role=admin, got %v", result.Edits[2].Properties["role"])
	}
}

func TestRevert_DoubleRevert_Returns409(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Employee", PrimaryKey: "emp-1"},
	}
	editsJSON, _ := json.Marshal(edits)

	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			5: {
				ID:     5,
				Edits:  editsJSON,
				Status: "REVERTED",
			},
		},
	}

	exec := NewExecutor(repo, nil)

	_, err := exec.Revert(context.Background(), "test-ontology", 5)
	if err == nil {
		t.Fatal("expected error for double revert")
	}
	var alreadyReverted *AlreadyRevertedError
	if !errors.As(err, &alreadyReverted) {
		t.Fatalf("expected *AlreadyRevertedError, got %T: %v", err, err)
	}
}

func TestRevert_NotFound(t *testing.T) {
	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{},
	}

	exec := NewExecutor(repo, nil)

	_, err := exec.Revert(context.Background(), "test-ontology", 999)
	if err == nil {
		t.Fatal("expected error for non-existent action log")
	}
}

func TestRevert_LinkEdits_Skipped(t *testing.T) {
	// LINK_CREATE and LINK_DELETE edits should be reversed.
	edits := []funnel.Edit{
		{Type: funnel.EditTypeLinkCreate, PrimaryKey: "emp-1", LinkTypeRID: "lt-1", TargetPrimaryKey: "dept-1"},
		{Type: funnel.EditTypeLinkDelete, PrimaryKey: "emp-2", LinkTypeRID: "lt-2", TargetPrimaryKey: "dept-2"},
	}
	prevEdits := []map[string]interface{}{nil, nil}
	editsJSON, _ := json.Marshal(edits)
	prevEditsJSON, _ := json.Marshal(prevEdits)

	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			6: {
				ID:        6,
				Edits:     editsJSON,
				PrevEdits: prevEditsJSON,
				Status:    "SUCCESS",
			},
		},
	}

	pub := &mockPublisher{}
	exec := NewExecutor(repo, pub)

	result, err := exec.Revert(context.Background(), "test-ontology", 6)
	if err != nil {
		t.Fatalf("Revert failed: %v", err)
	}
	if len(result.Edits) != 2 {
		t.Fatalf("expected 2 reverse edits, got %d", len(result.Edits))
	}
	// LINK_CREATE → LINK_DELETE
	if result.Edits[0].Type != funnel.EditTypeLinkDelete {
		t.Errorf("edit[0]: expected LINK_DELETE, got %s", result.Edits[0].Type)
	}
	// LINK_DELETE → LINK_CREATE
	if result.Edits[1].Type != funnel.EditTypeLinkCreate {
		t.Errorf("edit[1]: expected LINK_CREATE, got %s", result.Edits[1].Type)
	}
}

func TestRevert_PublishesBatch(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Employee", PrimaryKey: "emp-1", Properties: map[string]interface{}{"name": "Alice"}},
	}
	editsJSON, _ := json.Marshal(edits)

	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			7: {
				ID:     7,
				Edits:  editsJSON,
				Status: "SUCCESS",
			},
		},
	}

	pub := &mockPublisher{}
	exec := NewExecutor(repo, pub)

	_, err := exec.Revert(context.Background(), "test-ontology", 7)
	if err != nil {
		t.Fatalf("Revert failed: %v", err)
	}
	if pub.callCount != 1 {
		t.Errorf("expected 1 publish call, got %d", pub.callCount)
	}
	if pub.lastBatch == nil {
		t.Fatal("expected a published batch")
	}
	if pub.lastBatch.OntologyAPIName != "test-ontology" {
		t.Errorf("expected ontology=test-ontology, got %s", pub.lastBatch.OntologyAPIName)
	}
}

// ---------------------------------------------------------------------------
// Revert Handler Tests (US-105)
// ---------------------------------------------------------------------------

func TestRevertHandler_Success(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Employee", PrimaryKey: "emp-1"},
	}
	editsJSON, _ := json.Marshal(edits)

	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			1: {
				ID:     1,
				Edits:  editsJSON,
				Status: "SUCCESS",
			},
		},
	}

	pub := &mockPublisher{}
	exec := NewExecutor(repo, pub)
	handler := NewHandler(exec)

	body := bytes.NewBufferString(`{"actionLogId": 1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/actions/revert", body)
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("ontologyApiName", "test")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.Revert(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevertHandler_DoubleRevert_409(t *testing.T) {
	edits := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Employee", PrimaryKey: "emp-1"},
	}
	editsJSON, _ := json.Marshal(edits)

	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{
			1: {
				ID:     1,
				Edits:  editsJSON,
				Status: "REVERTED",
			},
		},
	}

	exec := NewExecutor(repo, nil)
	handler := NewHandler(exec)

	body := bytes.NewBufferString(`{"actionLogId": 1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/actions/revert", body)
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("ontologyApiName", "test")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.Revert(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRevertHandler_NotFound_404(t *testing.T) {
	repo := &mockOmsRepo{
		actionLogByID: map[int64]*oms.ActionLog{},
	}

	exec := NewExecutor(repo, nil)
	handler := NewHandler(exec)

	body := bytes.NewBufferString(`{"actionLogId": 999}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/test/actions/revert", body)
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("ontologyApiName", "test")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.Revert(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Value Type Constraint Enforcement (US-111)
// ---------------------------------------------------------------------------

// valueTypeAwareMockRepo extends mockOmsRepo with property + value type resolution.
type valueTypeAwareMockRepo struct {
	mockOmsRepo
	properties  map[string][]oms.Property  // objectType apiName → properties
	valueTypes  map[string]*oms.ValueType  // apiName → ValueType
	objectTypes map[string]*oms.ObjectType // apiName → ObjectType
}

func (m *valueTypeAwareMockRepo) GetObjectTypeByAPIName(_ context.Context, _, apiName string) (*oms.ObjectType, error) {
	if ot, ok := m.objectTypes[apiName]; ok {
		return ot, nil
	}
	return nil, oms.ErrNotFound
}

func (m *valueTypeAwareMockRepo) ListProperties(_ context.Context, objectTypeRID string) ([]oms.Property, error) {
	// Lookup by RID — iterate objectTypes to find matching RID → apiName → properties.
	for apiName, ot := range m.objectTypes {
		if ot.RID == objectTypeRID {
			return m.properties[apiName], nil
		}
	}
	return nil, nil
}

func (m *valueTypeAwareMockRepo) GetValueTypeByAPIName(_ context.Context, apiName string) (*oms.ValueType, error) {
	if vt, ok := m.valueTypes[apiName]; ok {
		return vt, nil
	}
	return nil, oms.ErrNotFound
}

func TestExecutor_ValueTypeConstraint_CreateObject_Pass(t *testing.T) {
	repo := &valueTypeAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createEmployee", []ParameterDef{
					{ID: "name", Type: "string", Required: true},
					{ID: "email", Type: "string", Required: true},
				}, []Rule{
					{Type: "createObject", ObjectType: "Employee", PropertyBindings: map[string]PropertyBinding{
						"name":  {Type: "parameter", Value: "name"},
						"email": {Type: "parameter", Value: "email"},
					}},
				}),
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"Employee": {RID: "ri.ontology.main.object-type.employee", APIName: "Employee"},
		},
		properties: map[string][]oms.Property{
			"Employee": {
				{APIName: "name", BaseType: "string"},
				{APIName: "email", BaseType: "string", TypeConfig: mustJSON(map[string]interface{}{
					"valueTypeApiName": "emailAddress",
				})},
			},
		},
		valueTypes: map[string]*oms.ValueType{
			"emailAddress": {
				RID:      "ri.ontology.main.value-type.email",
				APIName:  "emailAddress",
				BaseType: "string",
				Constraints: mustJSON(map[string]interface{}{
					"regex": `^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`,
				}),
			},
		},
	}

	exec := NewExecutor(repo, nil)
	result, err := exec.Prepare(context.Background(), "test-ontology", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{
			"name":  "Alice",
			"email": "alice@example.com",
		},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result == nil || len(result.Edits) == 0 {
		t.Fatal("expected edits in result")
	}
}

func TestExecutor_ValueTypeConstraint_CreateObject_Fail(t *testing.T) {
	repo := &valueTypeAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createEmployee", []ParameterDef{
					{ID: "name", Type: "string", Required: true},
					{ID: "email", Type: "string", Required: true},
				}, []Rule{
					{Type: "createObject", ObjectType: "Employee", PropertyBindings: map[string]PropertyBinding{
						"name":  {Type: "parameter", Value: "name"},
						"email": {Type: "parameter", Value: "email"},
					}},
				}),
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"Employee": {RID: "ri.ontology.main.object-type.employee", APIName: "Employee"},
		},
		properties: map[string][]oms.Property{
			"Employee": {
				{APIName: "name", BaseType: "string"},
				{APIName: "email", BaseType: "string", TypeConfig: mustJSON(map[string]interface{}{
					"valueTypeApiName": "emailAddress",
				})},
			},
		},
		valueTypes: map[string]*oms.ValueType{
			"emailAddress": {
				RID:      "ri.ontology.main.value-type.email",
				APIName:  "emailAddress",
				BaseType: "string",
				Constraints: mustJSON(map[string]interface{}{
					"regex": `^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`,
				}),
			},
		},
	}

	exec := NewExecutor(repo, nil)
	_, err := exec.Prepare(context.Background(), "test-ontology", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{
			"name":  "Alice",
			"email": "not-an-email",
		},
	})
	if err == nil {
		t.Fatal("expected constraint violation error")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Fatalf("error should mention field name 'email', got: %v", err)
	}
	if !strings.Contains(err.Error(), "regex") {
		t.Fatalf("error should mention constraint type, got: %v", err)
	}
}

func TestExecutor_ValueTypeConstraint_AdminPattern_Fail(t *testing.T) {
	repo := &valueTypeAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createEmployee", []ParameterDef{
					{ID: "name", Type: "string", Required: true},
					{ID: "email", Type: "string", Required: true},
				}, []Rule{
					{Type: "createObject", ObjectType: "Employee", PropertyBindings: map[string]PropertyBinding{
						"name":  {Type: "parameter", Value: "name"},
						"email": {Type: "parameter", Value: "email"},
					}},
				}),
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"Employee": {RID: "ri.ontology.main.object-type.employee", APIName: "Employee"},
		},
		properties: map[string][]oms.Property{
			"Employee": {
				{APIName: "name", BaseType: "string"},
				{APIName: "email", BaseType: "string", TypeConfig: mustJSON(map[string]interface{}{
					"valueTypeApiName": "emailAddress",
				})},
			},
		},
		valueTypes: map[string]*oms.ValueType{
			"emailAddress": {
				RID:      "ri.ontology.main.value-type.email",
				APIName:  "emailAddress",
				BaseType: "string",
				Constraints: mustJSON(map[string]interface{}{
					"pattern": `^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`,
				}),
			},
		},
	}

	exec := NewExecutor(repo, nil)
	_, err := exec.Prepare(context.Background(), "test-ontology", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{
			"name":  "Alice",
			"email": "not-an-email",
		},
	})
	if err == nil {
		t.Fatal("expected UI-authored pattern constraint violation error")
	}
	if !strings.Contains(err.Error(), "email") {
		t.Fatalf("error should mention field name 'email', got: %v", err)
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Fatalf("error should mention pattern constraint, got: %v", err)
	}
}

func TestExecutor_ValueTypeConstraint_ModifyObject_Fail(t *testing.T) {
	repo := &valueTypeAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("updateAge", []ParameterDef{
					{ID: "primaryKey", Type: "string", Required: true},
					{ID: "age", Type: "integer", Required: true},
				}, []Rule{
					{Type: "modifyObject", ObjectType: "Employee", PropertyBindings: map[string]PropertyBinding{
						"age": {Type: "parameter", Value: "age"},
					}},
				}),
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"Employee": {RID: "ri.ontology.main.object-type.employee", APIName: "Employee"},
		},
		properties: map[string][]oms.Property{
			"Employee": {
				{APIName: "primaryKey", BaseType: "string"},
				{APIName: "age", BaseType: "integer", TypeConfig: mustJSON(map[string]interface{}{
					"valueTypeApiName": "positiveAge",
				})},
			},
		},
		valueTypes: map[string]*oms.ValueType{
			"positiveAge": {
				RID:      "ri.ontology.main.value-type.age",
				APIName:  "positiveAge",
				BaseType: "integer",
				Constraints: mustJSON(map[string]interface{}{
					"min": 0,
					"max": 150,
				}),
			},
		},
	}

	exec := NewExecutor(repo, nil)
	_, err := exec.Prepare(context.Background(), "test-ontology", &ApplyRequest{
		ActionType: "updateAge",
		Parameters: map[string]interface{}{
			"primaryKey": "emp-1",
			"age":        float64(-5),
		},
	})
	if err == nil {
		t.Fatal("expected constraint violation error for negative age")
	}
	if !strings.Contains(err.Error(), "age") {
		t.Fatalf("error should mention field 'age', got: %v", err)
	}
	if !strings.Contains(err.Error(), "min") {
		t.Fatalf("error should mention min constraint, got: %v", err)
	}
}

func TestExecutor_ValueTypeConstraint_NoValueType_Passes(t *testing.T) {
	// Properties without valueTypeApiName in TypeConfig should not be validated.
	repo := &valueTypeAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createItem", []ParameterDef{
					{ID: "name", Type: "string", Required: true},
				}, []Rule{
					{Type: "createObject", ObjectType: "Item", PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
				}),
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"Item": {RID: "ri.ontology.main.object-type.item", APIName: "Item"},
		},
		properties: map[string][]oms.Property{
			"Item": {
				{APIName: "name", BaseType: "string"}, // no ValueType
			},
		},
		valueTypes: map[string]*oms.ValueType{},
	}

	exec := NewExecutor(repo, nil)
	result, err := exec.Prepare(context.Background(), "test-ontology", &ApplyRequest{
		ActionType: "createItem",
		Parameters: map[string]interface{}{
			"name": "anything goes",
		},
	})
	if err != nil {
		t.Fatalf("expected no error when no ValueType is defined, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
}

func TestExecutor_ValueTypeConstraint_EnumViolation(t *testing.T) {
	repo := &valueTypeAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createTicket", []ParameterDef{
					{ID: "title", Type: "string", Required: true},
					{ID: "priority", Type: "string", Required: true},
				}, []Rule{
					{Type: "createObject", ObjectType: "Ticket", PropertyBindings: map[string]PropertyBinding{
						"title":    {Type: "parameter", Value: "title"},
						"priority": {Type: "parameter", Value: "priority"},
					}},
				}),
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"Ticket": {RID: "ri.ontology.main.object-type.ticket", APIName: "Ticket"},
		},
		properties: map[string][]oms.Property{
			"Ticket": {
				{APIName: "title", BaseType: "string"},
				{APIName: "priority", BaseType: "string", TypeConfig: mustJSON(map[string]interface{}{
					"valueTypeApiName": "ticketPriority",
				})},
			},
		},
		valueTypes: map[string]*oms.ValueType{
			"ticketPriority": {
				RID:      "ri.ontology.main.value-type.priority",
				APIName:  "ticketPriority",
				BaseType: "string",
				Constraints: mustJSON(map[string]interface{}{
					"enum": []string{"low", "medium", "high", "critical"},
				}),
			},
		},
	}

	exec := NewExecutor(repo, nil)
	_, err := exec.Prepare(context.Background(), "test-ontology", &ApplyRequest{
		ActionType: "createTicket",
		Parameters: map[string]interface{}{
			"title":    "Bug report",
			"priority": "urgent", // not in enum
		},
	})
	if err == nil {
		t.Fatal("expected enum constraint violation error")
	}
	// US-208 changed the contract: enum failures now return a typed
	// *apierror.APIError with property + allowedValues in Parameters,
	// rather than the legacy "constraint validation: ... enum: ..." string.
	var apiErr *apierror.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *apierror.APIError, got %T: %v", err, err)
	}
	if apiErr.ErrorCode != "WEAVE_VALIDATION_ENUM" {
		t.Fatalf("ErrorCode = %q, want WEAVE_VALIDATION_ENUM", apiErr.ErrorCode)
	}
	if apiErr.Parameters["property"] != "priority" {
		t.Fatalf("Parameters.property = %q, want priority", apiErr.Parameters["property"])
	}
}

func TestExecutor_ValueTypeConstraint_NilProperty_Passes(t *testing.T) {
	// Nil property values should pass (nullability is handled by Validate, not constraints).
	repo := &valueTypeAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createEmployee", []ParameterDef{
					{ID: "name", Type: "string", Required: false}, // optional
				}, []Rule{
					{Type: "createObject", ObjectType: "Employee", PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					}},
				}),
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"Employee": {RID: "ri.ontology.main.object-type.employee", APIName: "Employee"},
		},
		properties: map[string][]oms.Property{
			"Employee": {
				{APIName: "name", BaseType: "string", TypeConfig: mustJSON(map[string]interface{}{
					"valueTypeApiName": "shortName",
				})},
			},
		},
		valueTypes: map[string]*oms.ValueType{
			"shortName": {
				RID:      "ri.ontology.main.value-type.shortname",
				APIName:  "shortName",
				BaseType: "string",
				Constraints: mustJSON(map[string]interface{}{
					"minLength": 1,
				}),
			},
		},
	}

	exec := NewExecutor(repo, nil)
	// name param is nil — should pass constraints (nullability is separate)
	result, err := exec.Prepare(context.Background(), "test-ontology", &ApplyRequest{
		ActionType: "createEmployee",
		Parameters: map[string]interface{}{
			"name": nil,
		},
	})
	if err != nil {
		t.Fatalf("nil value should pass constraint checks, got: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}
}

// ---------------------------------------------------------------------------
// US-208: Enum violation surfaces as WEAVE_VALIDATION_ENUM (HTTP 422)
// ---------------------------------------------------------------------------

// newEnumTicketRepo builds a ticket-priority valueTypeAwareMockRepo whose
// `priority` property is constrained to the enum {low, medium, high, critical}.
// The shape is reused across the executor + handler tests below.
func newEnumTicketRepo() *valueTypeAwareMockRepo {
	return &valueTypeAwareMockRepo{
		mockOmsRepo: mockOmsRepo{
			actionTypes: []oms.ActionType{
				newTestActionType("createTicket", []ParameterDef{
					{ID: "title", Type: "string", Required: true},
					{ID: "priority", Type: "string", Required: true},
				}, []Rule{
					{Type: "createObject", ObjectType: "Ticket", PropertyBindings: map[string]PropertyBinding{
						"title":    {Type: "parameter", Value: "title"},
						"priority": {Type: "parameter", Value: "priority"},
					}},
				}),
			},
		},
		objectTypes: map[string]*oms.ObjectType{
			"Ticket": {RID: "ri.ontology.main.object-type.ticket", APIName: "Ticket"},
		},
		properties: map[string][]oms.Property{
			"Ticket": {
				{APIName: "title", BaseType: "string"},
				{APIName: "priority", BaseType: "string", TypeConfig: mustJSON(map[string]interface{}{
					"valueTypeApiName": "ticketPriority",
				})},
			},
		},
		valueTypes: map[string]*oms.ValueType{
			"ticketPriority": {
				RID:      "ri.ontology.main.value-type.priority",
				APIName:  "ticketPriority",
				BaseType: "string",
				Constraints: mustJSON(map[string]interface{}{
					"enum": []string{"low", "medium", "high", "critical"},
				}),
			},
		},
	}
}

func TestExecutor_EnumViolation_ReturnsTypedAPIError422(t *testing.T) {
	exec := NewExecutor(newEnumTicketRepo(), nil)
	_, err := exec.Prepare(context.Background(), "test-ontology", &ApplyRequest{
		ActionType: "createTicket",
		Parameters: map[string]interface{}{
			"title":    "Bug report",
			"priority": "urgent",
		},
	})
	if err == nil {
		t.Fatal("expected enum violation error from Prepare")
	}
	var apiErr *apierror.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *apierror.APIError via errors.As, got %T: %v", err, err)
	}
	if apiErr.ErrorCode != "WEAVE_VALIDATION_ENUM" {
		t.Fatalf("ErrorCode = %q, want WEAVE_VALIDATION_ENUM", apiErr.ErrorCode)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("StatusCode = %d, want 422", apiErr.StatusCode)
	}
	if got := apiErr.Parameters["property"]; got != "priority" {
		t.Fatalf("Parameters.property = %q, want priority", got)
	}
	if got := apiErr.Parameters["value"]; got != "urgent" {
		t.Fatalf("Parameters.value = %q, want urgent", got)
	}
	allowed := apiErr.Parameters["allowedValues"]
	for _, want := range []string{"low", "medium", "high", "critical"} {
		if !strings.Contains(allowed, want) {
			t.Fatalf("Parameters.allowedValues = %q, want it to contain %q", allowed, want)
		}
	}
	if got := apiErr.Parameters["objectType"]; got != "Ticket" {
		t.Fatalf("Parameters.objectType = %q, want Ticket", got)
	}
}

func TestHandler_Apply_EnumViolation_422(t *testing.T) {
	exec := NewExecutor(newEnumTicketRepo(), nil)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	body := mustJSON(map[string]interface{}{
		"parameters": map[string]interface{}{
			"title":    "Bug report",
			"priority": "urgent",
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createTicket/apply",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ErrorCode  string            `json:"errorCode"`
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.ErrorCode != "WEAVE_VALIDATION_ENUM" {
		t.Fatalf("errorCode = %q, want WEAVE_VALIDATION_ENUM", resp.ErrorCode)
	}
	if resp.Parameters["property"] != "priority" {
		t.Fatalf("parameters.property = %q, want priority", resp.Parameters["property"])
	}
	allowed := resp.Parameters["allowedValues"]
	for _, want := range []string{"low", "medium", "high", "critical"} {
		if !strings.Contains(allowed, want) {
			t.Fatalf("parameters.allowedValues = %q, missing %q", allowed, want)
		}
	}
}

func TestHandler_ApplyBatch_EnumViolation_422(t *testing.T) {
	exec := NewExecutor(newEnumTicketRepo(), nil)
	handler := NewHandler(exec)
	router := setupRouter(handler)

	// Batch atomicity: one bad request fails the whole batch with the
	// underlying enum error code surfaced verbatim, not a generic 400.
	body := mustJSON(map[string]interface{}{
		"actions": []map[string]interface{}{
			{"parameters": map[string]interface{}{"title": "ok", "priority": "low"}},
			{"parameters": map[string]interface{}{"title": "bad", "priority": "urgent"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/ont-1/actions/createTicket/applyBatch",
		bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ErrorCode  string            `json:"errorCode"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if resp.ErrorCode != "WEAVE_VALIDATION_ENUM" {
		t.Fatalf("batch enum violation errorCode = %q, want WEAVE_VALIDATION_ENUM", resp.ErrorCode)
	}
	if resp.Parameters["property"] != "priority" {
		t.Fatalf("batch enum parameters.property = %q, want priority", resp.Parameters["property"])
	}
}
