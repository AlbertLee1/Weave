package oms

import (
	"encoding/json"
	"testing"
)

// Unit coverage for the Foundry LinkTypeSideV2 serializer that backs
// the outgoing/incoming link-type read surfaces. The BDD contract
// lives in handlers_link_type_side_v2_bdd_test.go; these tests pin
// the serializer branches (optional fields, FK parsing edge cases).

func TestLinkType_ForeignKeyPropertyAPIName(t *testing.T) {
	tests := []struct {
		name   string
		config json.RawMessage
		want   string
	}{
		{"fk config with sourceProperty", json.RawMessage(`{"sourceProperty":"departmentId","targetProperty":"id"}`), "departmentId"},
		{"nil config (M2M)", nil, ""},
		{"empty config", json.RawMessage(``), ""},
		{"invalid JSON", json.RawMessage(`{not-json`), ""},
		{"missing sourceProperty", json.RawMessage(`{"targetProperty":"id"}`), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lt := &LinkType{APIName: "worksIn", ForeignKeyConfig: tt.config}
			if got := lt.ForeignKeyPropertyAPIName(); got != tt.want {
				t.Errorf("ForeignKeyPropertyAPIName()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestLinkType_ToLinkTypeSideV2JSON(t *testing.T) {
	lt := &LinkType{
		RID:              "ri.ontology.main.link-type.worksIn",
		APIName:          "worksIn",
		DisplayName:      "Works In",
		Description:      "Employee works in a department",
		SourceObjectType: "ri.ontology.main.object-type.emp",
		TargetObjectType: "ri.ontology.main.object-type.dept",
		Cardinality:      "MANY_TO_ONE",
		ForeignKeyConfig: json.RawMessage(`{"sourceProperty":"departmentId","targetProperty":"id"}`),
		IsRequired:       true,
	}

	data, err := lt.ToLinkTypeSideV2JSON("Department")
	if err != nil {
		t.Fatalf("ToLinkTypeSideV2JSON: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Foundry LinkTypeSideV2 contract fields.
	expects := map[string]interface{}{
		"apiName":                   "worksIn",
		"displayName":               "Works In",
		"status":                    "ACTIVE",
		"objectTypeApiName":         "Department",
		"cardinality":               "MANY_TO_ONE",
		"linkTypeRid":               "ri.ontology.main.link-type.worksIn",
		"foreignKeyPropertyApiName": "departmentId",
		// Transition aliases + Weave extensions.
		"rid":                     "ri.ontology.main.link-type.worksIn",
		"linkedObjectTypeApiName": "Department",
		"required":                true,
		"description":             "Employee works in a department",
	}
	for k, want := range expects {
		if got := wire[k]; got != want {
			t.Errorf("wire[%q]=%v, want %v", k, got, want)
		}
	}
}

func TestLinkType_ToLinkTypeSideV2JSON_OmitsOptionalFields(t *testing.T) {
	lt := &LinkType{
		RID:              "ri.ontology.main.link-type.assignedTo",
		APIName:          "assignedTo",
		DisplayName:      "Assigned To",
		SourceObjectType: "ri.ontology.main.object-type.emp",
		TargetObjectType: "ri.ontology.main.object-type.proj",
		Cardinality:      "MANY_TO_MANY",
		JoinTableConfig:  json.RawMessage(`{"table":"emp_proj"}`),
	}

	data, err := lt.ToLinkTypeSideV2JSON("Project")
	if err != nil {
		t.Fatalf("ToLinkTypeSideV2JSON: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, k := range []string{"foreignKeyPropertyApiName", "description", "inverseLinkRid", "propagateMarkings", "typeClasses"} {
		if v, present := wire[k]; present {
			t.Errorf("wire[%q]=%v, want omitted", k, v)
		}
	}
}

func TestLinkType_ToLinkTypeSideV2JSON_PassesThroughOptionalExtensions(t *testing.T) {
	lt := &LinkType{
		RID:               "ri.ontology.main.link-type.worksIn",
		APIName:           "worksIn",
		DisplayName:       "Works In",
		Cardinality:       "MANY_TO_ONE",
		InverseLinkRID:    "ri.ontology.main.link-type.hasEmployees",
		PropagateMarkings: true,
		TypeClasses:       []string{VertexLinkTypeClassPrimaryDirection},
	}

	data, err := lt.ToLinkTypeSideV2JSON("Department")
	if err != nil {
		t.Fatalf("ToLinkTypeSideV2JSON: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got := wire["inverseLinkRid"]; got != "ri.ontology.main.link-type.hasEmployees" {
		t.Errorf("inverseLinkRid=%v, want ri.ontology.main.link-type.hasEmployees", got)
	}
	if got := wire["propagateMarkings"]; got != true {
		t.Errorf("propagateMarkings=%v, want true", got)
	}
	tcs, ok := wire["typeClasses"].([]interface{})
	if !ok || len(tcs) != 1 || tcs[0] != VertexLinkTypeClassPrimaryDirection {
		t.Errorf("typeClasses=%v, want [%s]", wire["typeClasses"], VertexLinkTypeClassPrimaryDirection)
	}
}
