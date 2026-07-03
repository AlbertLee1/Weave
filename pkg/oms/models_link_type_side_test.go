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

	data, err := lt.ToLinkTypeSideV2JSON("Department", OutgoingLinkSide)
	if err != nil {
		t.Fatalf("ToLinkTypeSideV2JSON: %v", err)
	}
	var wire map[string]interface{}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Foundry LinkTypeSideV2 contract fields. cardinality is the Foundry
	// LinkTypeSideCardinality (ONE | MANY) for the linked side: a
	// MANY_TO_ONE link seen from its source (outgoing) reaches ONE target.
	expects := map[string]interface{}{
		"apiName":                   "worksIn",
		"displayName":               "Works In",
		"status":                    "ACTIVE",
		"objectTypeApiName":         "Department",
		"cardinality":               "ONE",
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

// TestLinkType_ToLinkTypeSideV2JSON_Cardinality pins the Weave→Foundry
// cardinality mapping across all four Weave SOURCE_TO_TARGET values and
// both link-side directions. Foundry's LinkTypeSideCardinality is
// ONE | MANY and names the multiplicity of the LINKED (far) side as
// seen from the queried object type:
//   - outgoing (queried = source, far = target): use the target token.
//   - incoming (queried = target, far = source): use the source token.
func TestLinkType_ToLinkTypeSideV2JSON_Cardinality(t *testing.T) {
	cases := []struct {
		weave    string
		dir      LinkSideDirection
		wantCard string
	}{
		// Outgoing: far end is the target token.
		{"ONE_TO_ONE", OutgoingLinkSide, "ONE"},
		{"ONE_TO_MANY", OutgoingLinkSide, "MANY"},
		{"MANY_TO_ONE", OutgoingLinkSide, "ONE"},
		{"MANY_TO_MANY", OutgoingLinkSide, "MANY"},
		// Incoming: far end is the source token.
		{"ONE_TO_ONE", IncomingLinkSide, "ONE"},
		{"ONE_TO_MANY", IncomingLinkSide, "ONE"},
		{"MANY_TO_ONE", IncomingLinkSide, "MANY"},
		{"MANY_TO_MANY", IncomingLinkSide, "MANY"},
	}
	dirName := map[LinkSideDirection]string{OutgoingLinkSide: "outgoing", IncomingLinkSide: "incoming"}
	for _, tc := range cases {
		t.Run(tc.weave+"_"+dirName[tc.dir], func(t *testing.T) {
			lt := &LinkType{
				RID:         "ri.ontology.main.link-type.x",
				APIName:     "x",
				DisplayName: "X",
				Cardinality: tc.weave,
			}
			data, err := lt.ToLinkTypeSideV2JSON("Far", tc.dir)
			if err != nil {
				t.Fatalf("ToLinkTypeSideV2JSON: %v", err)
			}
			var wire map[string]interface{}
			if err := json.Unmarshal(data, &wire); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := wire["cardinality"]; got != tc.wantCard {
				t.Errorf("%s %s: cardinality=%v, want %s (Foundry LinkTypeSideCardinality)",
					tc.weave, dirName[tc.dir], got, tc.wantCard)
			}
		})
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

	data, err := lt.ToLinkTypeSideV2JSON("Project", OutgoingLinkSide)
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

	data, err := lt.ToLinkTypeSideV2JSON("Department", OutgoingLinkSide)
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
