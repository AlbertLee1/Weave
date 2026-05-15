package functionactions_test

import (
	"errors"
	"sort"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/functionactions"
)

func TestResolveActionMode_Given_FunctionBackedActionType_When_Resolve_Then_ModeFunctionBacked(t *testing.T) {
	at := &oms.ActionType{IsFunctionBacked: true, FunctionRID: "ri.ontology.main.function.f1"}
	if got := functionactions.ResolveActionMode(at); got != functionactions.ActionModeFunctionBacked {
		t.Errorf("mode: got %q, want %q", got, functionactions.ActionModeFunctionBacked)
	}
}

func TestResolveActionMode_Given_StandardActionType_When_Resolve_Then_ModeStandard(t *testing.T) {
	at := &oms.ActionType{IsFunctionBacked: false}
	if got := functionactions.ResolveActionMode(at); got != functionactions.ActionModeStandard {
		t.Errorf("mode: got %q, want %q", got, functionactions.ActionModeStandard)
	}
}

func TestResolveActionMode_Given_FunctionBackedFlagButEmptyRID_When_Resolve_Then_ModeStandard(t *testing.T) {
	at := &oms.ActionType{IsFunctionBacked: true, FunctionRID: ""}
	if got := functionactions.ResolveActionMode(at); got != functionactions.ActionModeStandard {
		t.Errorf("mode: got %q, want %q", got, functionactions.ActionModeStandard)
	}
}

func TestResolveActionMode_Given_NilActionType_When_Resolve_Then_ModeStandard(t *testing.T) {
	if got := functionactions.ResolveActionMode(nil); got != functionactions.ActionModeStandard {
		t.Errorf("mode: got %q, want %q", got, functionactions.ActionModeStandard)
	}
}

func TestValidateOutputMappings_Given_EmptySlice_When_Validate_Then_NoError(t *testing.T) {
	if err := functionactions.ValidateOutputMappings(nil); err != nil {
		t.Errorf("expected nil error for empty mappings, got %v", err)
	}
}

func TestValidateOutputMappings_Given_MissingOutputField_When_Validate_Then_MappingError(t *testing.T) {
	mappings := []functionactions.OutputMapping{{
		ObjectType:          "Flight",
		PrimaryKeyParameter: "flightId",
		Property:            "delay",
	}}
	err := functionactions.ValidateOutputMappings(mappings)
	var me *functionactions.MappingError
	if !errors.As(err, &me) {
		t.Fatalf("expected MappingError, got %T: %v", err, err)
	}
	if me.Reason == "" || me.Field == "" {
		t.Errorf("expected populated Field+Reason, got %+v", me)
	}
}

func TestValidateOutputMappings_Given_MissingObjectType_When_Validate_Then_MappingError(t *testing.T) {
	mappings := []functionactions.OutputMapping{{
		OutputField:         "delay",
		PrimaryKeyParameter: "flightId",
		Property:            "delay",
	}}
	if err := functionactions.ValidateOutputMappings(mappings); err == nil {
		t.Errorf("expected error for missing objectType, got nil")
	}
}

func TestValidateOutputMappings_Given_MissingPrimaryKeyParameter_When_Validate_Then_MappingError(t *testing.T) {
	mappings := []functionactions.OutputMapping{{
		OutputField: "delay",
		ObjectType:  "Flight",
		Property:    "delay",
	}}
	if err := functionactions.ValidateOutputMappings(mappings); err == nil {
		t.Errorf("expected error for missing primaryKeyParameter, got nil")
	}
}

func TestValidateOutputMappings_Given_MissingProperty_When_Validate_Then_MappingError(t *testing.T) {
	mappings := []functionactions.OutputMapping{{
		OutputField:         "delay",
		ObjectType:          "Flight",
		PrimaryKeyParameter: "flightId",
	}}
	if err := functionactions.ValidateOutputMappings(mappings); err == nil {
		t.Errorf("expected error for missing property, got nil")
	}
}

func TestMapFunctionOutputToScenarioEdits_Given_SingleMappingWithStringPK_When_Map_Then_ModifyPropertyEdit(t *testing.T) {
	out := map[string]interface{}{"predictedDelayMin": 12.4}
	params := map[string]interface{}{"flightId": "FL-001"}
	mappings := []functionactions.OutputMapping{{
		OutputField:         "predictedDelayMin",
		ObjectType:          "Flight",
		PrimaryKeyParameter: "flightId",
		Property:            "predictedDelay",
	}}
	edits, err := functionactions.MapFunctionOutputToScenarioEdits(out, params, mappings)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("edits: got %d, want 1", len(edits))
	}
	got := edits[0]
	if got.Op != functionactions.OpModifyProperty {
		t.Errorf("op: got %q, want %q", got.Op, functionactions.OpModifyProperty)
	}
	if got.ObjectType != "Flight" || got.ObjectID != "FL-001" || got.Property != "predictedDelay" {
		t.Errorf("target: got %+v, want Flight/FL-001/predictedDelay", got)
	}
	if got.NewValue != 12.4 {
		t.Errorf("newValue: got %v, want 12.4", got.NewValue)
	}
}

func TestMapFunctionOutputToScenarioEdits_Given_MultipleMappings_When_Map_Then_AllEditsInOrder(t *testing.T) {
	out := map[string]interface{}{
		"predictedDelay": 12.4,
		"riskScore":      0.7,
	}
	params := map[string]interface{}{"flightId": "FL-001"}
	mappings := []functionactions.OutputMapping{
		{OutputField: "predictedDelay", ObjectType: "Flight", PrimaryKeyParameter: "flightId", Property: "predictedDelay"},
		{OutputField: "riskScore", ObjectType: "Flight", PrimaryKeyParameter: "flightId", Property: "riskScore"},
	}
	edits, err := functionactions.MapFunctionOutputToScenarioEdits(out, params, mappings)
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	if len(edits) != 2 {
		t.Fatalf("edits: got %d, want 2", len(edits))
	}
	if edits[0].Property != "predictedDelay" || edits[1].Property != "riskScore" {
		t.Errorf("edit order not preserved: %+v", edits)
	}
}

func TestMapFunctionOutputToScenarioEdits_Given_MissingOutputField_When_Map_Then_MappingError(t *testing.T) {
	out := map[string]interface{}{"somethingElse": 1.0}
	params := map[string]interface{}{"flightId": "FL-001"}
	mappings := []functionactions.OutputMapping{{
		OutputField:         "predictedDelay",
		ObjectType:          "Flight",
		PrimaryKeyParameter: "flightId",
		Property:            "predictedDelay",
	}}
	_, err := functionactions.MapFunctionOutputToScenarioEdits(out, params, mappings)
	var me *functionactions.MappingError
	if !errors.As(err, &me) {
		t.Fatalf("expected MappingError, got %T: %v", err, err)
	}
	if me.Field != "predictedDelay" {
		t.Errorf("Field: got %q, want %q", me.Field, "predictedDelay")
	}
}

func TestMapFunctionOutputToScenarioEdits_Given_MissingPrimaryKeyParam_When_Map_Then_MappingError(t *testing.T) {
	out := map[string]interface{}{"predictedDelay": 12.4}
	params := map[string]interface{}{"someOtherParam": "FL-001"}
	mappings := []functionactions.OutputMapping{{
		OutputField:         "predictedDelay",
		ObjectType:          "Flight",
		PrimaryKeyParameter: "flightId",
		Property:            "predictedDelay",
	}}
	_, err := functionactions.MapFunctionOutputToScenarioEdits(out, params, mappings)
	if err == nil {
		t.Fatalf("expected error when PK param missing, got nil")
	}
	var me *functionactions.MappingError
	if !errors.As(err, &me) {
		t.Fatalf("expected MappingError, got %T: %v", err, err)
	}
	if me.Field != "flightId" {
		t.Errorf("Field: got %q, want %q", me.Field, "flightId")
	}
}

func TestMapFunctionOutputToScenarioEdits_Given_NonStringPK_When_Map_Then_MappingError(t *testing.T) {
	out := map[string]interface{}{"predictedDelay": 12.4}
	params := map[string]interface{}{"flightId": 42}
	mappings := []functionactions.OutputMapping{{
		OutputField:         "predictedDelay",
		ObjectType:          "Flight",
		PrimaryKeyParameter: "flightId",
		Property:            "predictedDelay",
	}}
	_, err := functionactions.MapFunctionOutputToScenarioEdits(out, params, mappings)
	if err == nil {
		t.Fatalf("expected error when PK param non-string, got nil")
	}
}

func TestMapFunctionOutputToScenarioEdits_Given_EmptyStringPK_When_Map_Then_MappingError(t *testing.T) {
	out := map[string]interface{}{"predictedDelay": 12.4}
	params := map[string]interface{}{"flightId": ""}
	mappings := []functionactions.OutputMapping{{
		OutputField:         "predictedDelay",
		ObjectType:          "Flight",
		PrimaryKeyParameter: "flightId",
		Property:            "predictedDelay",
	}}
	_, err := functionactions.MapFunctionOutputToScenarioEdits(out, params, mappings)
	if err == nil {
		t.Fatalf("expected error when PK param is empty string, got nil")
	}
}

func TestMapFunctionOutputToScenarioEdits_Given_InvalidMapping_When_Map_Then_ValidationError(t *testing.T) {
	out := map[string]interface{}{"predictedDelay": 12.4}
	params := map[string]interface{}{"flightId": "FL-001"}
	mappings := []functionactions.OutputMapping{{
		// Missing Property.
		OutputField:         "predictedDelay",
		ObjectType:          "Flight",
		PrimaryKeyParameter: "flightId",
	}}
	if _, err := functionactions.MapFunctionOutputToScenarioEdits(out, params, mappings); err == nil {
		t.Fatalf("expected validation error for under-specified mapping, got nil")
	}
}

func TestFunnelEditsToScenarioEdits_Given_CreateModifyDelete_When_Convert_Then_MatchingScenarioOps(t *testing.T) {
	in := []funnel.Edit{
		{Type: funnel.EditTypeCreate, ObjectType: "Flight", PrimaryKey: "FL-001", Properties: map[string]interface{}{"name": "FL-001"}},
		{Type: funnel.EditTypeModify, ObjectType: "Flight", PrimaryKey: "FL-001", Properties: map[string]interface{}{"status": "delayed", "delay": 12.4}},
		{Type: funnel.EditTypeDelete, ObjectType: "Flight", PrimaryKey: "FL-002"},
	}
	got := functionactions.FunnelEditsToScenarioEdits(in)
	// 1 createObject + 2 modifyProperty (one per property) + 1 deleteObject = 4
	if len(got) != 4 {
		t.Fatalf("edits: got %d, want 4 — %+v", len(got), got)
	}
	if got[0].Op != functionactions.OpCreateObject || got[0].ObjectID != "FL-001" {
		t.Errorf("create: got %+v", got[0])
	}
	if got[3].Op != functionactions.OpDeleteObject || got[3].ObjectID != "FL-002" {
		t.Errorf("delete: got %+v", got[3])
	}
	props := []string{got[1].Property, got[2].Property}
	sort.Strings(props)
	if props[0] != "delay" || props[1] != "status" {
		t.Errorf("modifyProperty fan-out: got %v, want [delay status]", props)
	}
}

func TestFunnelEditsToScenarioEdits_Given_ModifyWithNoProperties_When_Convert_Then_Skipped(t *testing.T) {
	in := []funnel.Edit{
		{Type: funnel.EditTypeModify, ObjectType: "Flight", PrimaryKey: "FL-001"},
	}
	if got := functionactions.FunnelEditsToScenarioEdits(in); len(got) != 0 {
		t.Errorf("expected no edits for empty MODIFY, got %+v", got)
	}
}

func TestFunnelEditsToScenarioEdits_Given_LinkEdits_When_Convert_Then_AddDeleteLinkOps(t *testing.T) {
	in := []funnel.Edit{
		{Type: funnel.EditTypeLinkCreate, LinkTypeRID: "ri.ontology.main.link-type.l1", PrimaryKey: "A", TargetPrimaryKey: "B"},
		{Type: funnel.EditTypeLinkDelete, LinkTypeRID: "ri.ontology.main.link-type.l1", PrimaryKey: "A", TargetPrimaryKey: "B"},
	}
	got := functionactions.FunnelEditsToScenarioEdits(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 link edits, got %d", len(got))
	}
	if got[0].Op != functionactions.OpAddLink || got[0].SrcID != "A" || got[0].DstID != "B" {
		t.Errorf("addLink: got %+v", got[0])
	}
	if got[1].Op != functionactions.OpDeleteLink {
		t.Errorf("deleteLink: got %+v", got[1])
	}
}

func TestFunnelEditsToScenarioEdits_Given_EmptyInput_When_Convert_Then_Nil(t *testing.T) {
	if got := functionactions.FunnelEditsToScenarioEdits(nil); got != nil {
		t.Errorf("expected nil for empty input, got %+v", got)
	}
}

func TestNewFunctionActionRID_Given_NoArgs_When_Generate_Then_VertexNamespace(t *testing.T) {
	rid := functionactions.NewFunctionActionRID()
	want := "ri.vertex.main.function-action."
	if len(rid) <= len(want) || rid[:len(want)] != want {
		t.Errorf("RID prefix: got %q, want prefix %q", rid, want)
	}
}
