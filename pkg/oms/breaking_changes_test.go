package oms_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// stubSavedObjectSetLister returns a fixed slice for tests; nil result mimics
// the degraded-mode wiring (no SavedStore plugged in).
type stubSavedObjectSetLister struct {
	sets []oms.SavedObjectSetRef
	err  error
}

func (s *stubSavedObjectSetLister) ListSavedObjectSets(_ context.Context, _ string) ([]oms.SavedObjectSetRef, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.sets, nil
}

func mustJSON(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func findChange(report oms.BreakingChangesReport, kind, propAPIName string) *oms.BreakingChange {
	for i := range report.Changes {
		c := &report.Changes[i]
		if c.Kind == kind && c.PropertyAPIName == propAPIName {
			return c
		}
	}
	return nil
}

func findChangeForOT(report oms.BreakingChangesReport, kind, otAPIName string) *oms.BreakingChange {
	for i := range report.Changes {
		c := &report.Changes[i]
		if c.Kind == kind && c.ObjectTypeAPIName == otAPIName {
			return c
		}
	}
	return nil
}

func setupBaseRepo() *mockRepo {
	return &mockRepo{
		ontologies: []oms.Ontology{
			{RID: "ri.ontology.main.ontology.1", APIName: "test", DisplayName: "Test"},
		},
		objectTypes: []oms.ObjectType{
			{RID: "ot-emp", OntologyRID: "ri.ontology.main.ontology.1", APIName: "employee", DisplayName: "Employee", PrimaryKey: "employeeId", PrimaryKeys: []string{"employeeId"}, Status: "ACTIVE", Visibility: "NORMAL"},
		},
		properties: []oms.Property{
			{RID: "p-name", ObjectTypeRID: "ot-emp", APIName: "fullName", BaseType: "string", IsNullable: true},
			{RID: "p-age", ObjectTypeRID: "ot-emp", APIName: "age", BaseType: "integer", IsNullable: true},
		},
		branches: []oms.OntologyBranch{
			{ID: "br-1", OntologyRID: "ri.ontology.main.ontology.1", Name: "feature/break", Status: "open", BaseVersion: 1},
		},
	}
}

func TestDetectBreakingChanges_NoChanges_EmptyReport(t *testing.T) {
	repo := setupBaseRepo()
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if rep.BranchID != "br-1" {
		t.Errorf("expected branchId=br-1, got %s", rep.BranchID)
	}
	if len(rep.Changes) != 0 {
		t.Errorf("expected 0 changes, got %d", len(rep.Changes))
	}
}

func TestDetectBreakingChanges_PropertyDeleted(t *testing.T) {
	repo := setupBaseRepo()
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-1", BranchID: "br-1", ChangeType: "DELETED", EntityType: "property", EntityRID: "p-name",
			BeforeState: mustJSON(t, repo.properties[0]),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	c := findChange(rep, oms.BreakingChangeKindPropertyDeleted, "fullName")
	if c == nil {
		t.Fatalf("expected PROPERTY_DELETED for fullName, got %+v", rep)
	}
	if c.ObjectTypeAPIName != "employee" {
		t.Errorf("expected ObjectTypeAPIName=employee, got %s", c.ObjectTypeAPIName)
	}
	if c.ObjectTypeRID != "ot-emp" {
		t.Errorf("expected ObjectTypeRID=ot-emp, got %s", c.ObjectTypeRID)
	}
}

func TestDetectBreakingChanges_PropertyTypeNarrowed(t *testing.T) {
	repo := setupBaseRepo()
	// before: integer; after: string is a type change → narrowing risk.
	after := repo.properties[1]
	after.BaseType = "string"
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-2", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "property", EntityRID: "p-age",
			BeforeState: mustJSON(t, repo.properties[1]),
			AfterState:  mustJSON(t, after),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	c := findChange(rep, oms.BreakingChangeKindPropertyTypeNarrowed, "age")
	if c == nil {
		t.Fatalf("expected PROPERTY_TYPE_NARROWED for age, got %+v", rep)
	}
	if c.Detail == "" {
		t.Errorf("expected Detail to describe type change")
	}
}

func TestDetectBreakingChanges_PropertyTypeNarrowed_ArrayChange(t *testing.T) {
	repo := setupBaseRepo()
	after := repo.properties[0]
	after.IsArray = true
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-arr", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "property", EntityRID: "p-name",
			BeforeState: mustJSON(t, repo.properties[0]),
			AfterState:  mustJSON(t, after),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if findChange(rep, oms.BreakingChangeKindPropertyTypeNarrowed, "fullName") == nil {
		t.Fatalf("expected PROPERTY_TYPE_NARROWED for fullName (array change), got %+v", rep)
	}
}

func TestDetectBreakingChanges_PropertyRequiredAdded(t *testing.T) {
	repo := setupBaseRepo()
	// before: nullable; after: not nullable → required-added.
	after := repo.properties[0]
	after.IsNullable = false
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-3", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "property", EntityRID: "p-name",
			BeforeState: mustJSON(t, repo.properties[0]),
			AfterState:  mustJSON(t, after),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if findChange(rep, oms.BreakingChangeKindPropertyRequiredAdded, "fullName") == nil {
		t.Fatalf("expected PROPERTY_REQUIRED_ADDED for fullName, got %+v", rep)
	}
}

func TestDetectBreakingChanges_RequiredRelaxed_NotBreaking(t *testing.T) {
	repo := setupBaseRepo()
	// before: not nullable; after: nullable → not breaking (relaxation).
	repo.properties[0].IsNullable = false
	after := repo.properties[0]
	after.IsNullable = true
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-4", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "property", EntityRID: "p-name",
			BeforeState: mustJSON(t, repo.properties[0]),
			AfterState:  mustJSON(t, after),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(rep.Changes) != 0 {
		t.Errorf("expected no changes for required→nullable, got %+v", rep)
	}
}

func TestDetectBreakingChanges_PrimaryKeyChanged(t *testing.T) {
	repo := setupBaseRepo()
	after := repo.objectTypes[0]
	after.PrimaryKey = "newKey"
	after.PrimaryKeys = []string{"newKey"}
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-5", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ot-emp",
			BeforeState: mustJSON(t, repo.objectTypes[0]),
			AfterState:  mustJSON(t, after),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	c := findChangeForOT(rep, oms.BreakingChangeKindPrimaryKeyChanged, "employee")
	if c == nil {
		t.Fatalf("expected PRIMARY_KEY_CHANGED for employee, got %+v", rep)
	}
	if c.PropertyAPIName != "" {
		t.Errorf("expected empty PropertyAPIName for PK change, got %q", c.PropertyAPIName)
	}
}

func TestDetectBreakingChanges_PrimaryKeyUnchanged_NoFalsePositive(t *testing.T) {
	repo := setupBaseRepo()
	// Change DisplayName only — should NOT emit PRIMARY_KEY_CHANGED.
	after := repo.objectTypes[0]
	after.DisplayName = "Renamed Employee"
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-6", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ot-emp",
			BeforeState: mustJSON(t, repo.objectTypes[0]),
			AfterState:  mustJSON(t, after),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(rep.Changes) != 0 {
		t.Errorf("expected no breaking changes for displayName-only edit, got %+v", rep)
	}
}

func TestDetectBreakingChanges_AffectedActionTypes(t *testing.T) {
	repo := setupBaseRepo()
	// Action references the fullName property via propertyBindings.
	rules := mustJSON(t, []map[string]interface{}{
		{
			"type":       "modifyObject",
			"objectType": "employee",
			"propertyBindings": map[string]interface{}{
				"fullName": map[string]interface{}{"type": "parameter", "value": "name"},
			},
		},
	})
	// Action that does NOT reference fullName — shouldn't be marked affected.
	otherRules := mustJSON(t, []map[string]interface{}{
		{
			"type":       "modifyObject",
			"objectType": "employee",
			"propertyBindings": map[string]interface{}{
				"age": map[string]interface{}{"type": "parameter", "value": "ageParam"},
			},
		},
	})
	repo.actionTypes = []oms.ActionType{
		{RID: "act-rename", OntologyRID: "ri.ontology.main.ontology.1", APIName: "renameEmp", DisplayName: "Rename", Status: "ACTIVE", Rules: rules},
		{RID: "act-age", OntologyRID: "ri.ontology.main.ontology.1", APIName: "agePush", DisplayName: "Age", Status: "ACTIVE", Rules: otherRules},
	}
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-7", BranchID: "br-1", ChangeType: "DELETED", EntityType: "property", EntityRID: "p-name",
			BeforeState: mustJSON(t, repo.properties[0]),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	c := findChange(rep, oms.BreakingChangeKindPropertyDeleted, "fullName")
	if c == nil {
		t.Fatalf("expected PROPERTY_DELETED, got %+v", rep)
	}
	if len(c.AffectedActionTypes) != 1 || c.AffectedActionTypes[0] != "act-rename" {
		t.Errorf("expected AffectedActionTypes=[act-rename], got %v", c.AffectedActionTypes)
	}
}

func TestDetectBreakingChanges_AffectedActionTypes_PrimaryKeyChange(t *testing.T) {
	repo := setupBaseRepo()
	rules := mustJSON(t, []map[string]interface{}{
		{"type": "modifyObject", "objectType": "employee", "propertyBindings": map[string]interface{}{}},
	})
	repo.actionTypes = []oms.ActionType{
		{RID: "act-mod", OntologyRID: "ri.ontology.main.ontology.1", APIName: "modEmp", DisplayName: "Mod", Status: "ACTIVE", Rules: rules},
	}
	after := repo.objectTypes[0]
	after.PrimaryKey = "uuid"
	after.PrimaryKeys = []string{"uuid"}
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-pk", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ot-emp",
			BeforeState: mustJSON(t, repo.objectTypes[0]),
			AfterState:  mustJSON(t, after),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	c := findChangeForOT(rep, oms.BreakingChangeKindPrimaryKeyChanged, "employee")
	if c == nil {
		t.Fatalf("expected PK change, got %+v", rep)
	}
	if len(c.AffectedActionTypes) != 1 || c.AffectedActionTypes[0] != "act-mod" {
		t.Errorf("expected AffectedActionTypes=[act-mod], got %v", c.AffectedActionTypes)
	}
}

func TestDetectBreakingChanges_AffectedSavedObjectSets_FilterField(t *testing.T) {
	repo := setupBaseRepo()
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-8", BranchID: "br-1", ChangeType: "DELETED", EntityType: "property", EntityRID: "p-name",
			BeforeState: mustJSON(t, repo.properties[0]),
		},
	}
	// SavedObjectSet that filters on fullName.
	defWithFullName := mustJSON(t, map[string]interface{}{
		"type": "filter",
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"where": map[string]interface{}{
			"type":  "eq",
			"field": "fullName",
			"value": "Alice",
		},
	})
	defWithoutRef := mustJSON(t, map[string]interface{}{
		"type":       "base",
		"objectType": "department",
	})
	lister := &stubSavedObjectSetLister{
		sets: []oms.SavedObjectSetRef{
			{ID: "sos-1", Definition: defWithFullName},
			{ID: "sos-2", Definition: defWithoutRef},
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, lister, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	c := findChange(rep, oms.BreakingChangeKindPropertyDeleted, "fullName")
	if c == nil {
		t.Fatalf("expected PROPERTY_DELETED, got %+v", rep)
	}
	if len(c.AffectedSavedObjectSets) != 1 || c.AffectedSavedObjectSets[0] != "sos-1" {
		t.Errorf("expected AffectedSavedObjectSets=[sos-1], got %v", c.AffectedSavedObjectSets)
	}
}

func TestDetectBreakingChanges_AffectedSavedObjectSets_NestedAndOr(t *testing.T) {
	repo := setupBaseRepo()
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-9", BranchID: "br-1", ChangeType: "DELETED", EntityType: "property", EntityRID: "p-age",
			BeforeState: mustJSON(t, repo.properties[1]),
		},
	}
	// SavedObjectSet with a nested `and` referencing age.
	def := mustJSON(t, map[string]interface{}{
		"type": "filter",
		"objectSet": map[string]interface{}{
			"type":       "base",
			"objectType": "employee",
		},
		"where": map[string]interface{}{
			"type": "and",
			"value": []map[string]interface{}{
				{"type": "eq", "field": "fullName", "value": "Alice"},
				{"type": "gt", "field": "age", "value": 30},
			},
		},
	})
	lister := &stubSavedObjectSetLister{
		sets: []oms.SavedObjectSetRef{{ID: "sos-nested", Definition: def}},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, lister, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	c := findChange(rep, oms.BreakingChangeKindPropertyDeleted, "age")
	if c == nil {
		t.Fatalf("expected PROPERTY_DELETED for age, got %+v", rep)
	}
	if len(c.AffectedSavedObjectSets) != 1 || c.AffectedSavedObjectSets[0] != "sos-nested" {
		t.Errorf("expected nested filter to flag sos-nested, got %v", c.AffectedSavedObjectSets)
	}
}

func TestDetectBreakingChanges_AffectedSavedObjectSets_PrimaryKeyOnBaseReference(t *testing.T) {
	repo := setupBaseRepo()
	after := repo.objectTypes[0]
	after.PrimaryKey = "uuid"
	after.PrimaryKeys = []string{"uuid"}
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-10", BranchID: "br-1", ChangeType: "MODIFIED", EntityType: "objectType", EntityRID: "ot-emp",
			BeforeState: mustJSON(t, repo.objectTypes[0]),
			AfterState:  mustJSON(t, after),
		},
	}
	def := mustJSON(t, map[string]interface{}{
		"type":       "base",
		"objectType": "employee",
	})
	lister := &stubSavedObjectSetLister{
		sets: []oms.SavedObjectSetRef{{ID: "sos-base", Definition: def}},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, lister, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	c := findChangeForOT(rep, oms.BreakingChangeKindPrimaryKeyChanged, "employee")
	if c == nil {
		t.Fatalf("expected PK change, got %+v", rep)
	}
	if len(c.AffectedSavedObjectSets) != 1 || c.AffectedSavedObjectSets[0] != "sos-base" {
		t.Errorf("expected sos-base affected by PK change, got %v", c.AffectedSavedObjectSets)
	}
}

func TestDetectBreakingChanges_NilSavedSetsLister_OK(t *testing.T) {
	repo := setupBaseRepo()
	repo.branchChanges = []oms.BranchChange{
		{
			ID: "bc-11", BranchID: "br-1", ChangeType: "DELETED", EntityType: "property", EntityRID: "p-name",
			BeforeState: mustJSON(t, repo.properties[0]),
		},
	}
	rep, err := oms.DetectBreakingChanges(context.Background(), repo, nil, "ri.ontology.main.ontology.1", "test", "br-1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	c := findChange(rep, oms.BreakingChangeKindPropertyDeleted, "fullName")
	if c == nil {
		t.Fatal("expected detected change even without saved-set lister")
	}
	if len(c.AffectedSavedObjectSets) != 0 {
		t.Errorf("expected no AffectedSavedObjectSets when lister is nil, got %v", c.AffectedSavedObjectSets)
	}
}
