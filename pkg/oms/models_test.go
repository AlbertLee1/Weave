package oms

import (
	"encoding/json"
	"testing"
)

func TestOntology_JSON(t *testing.T) {
	o := Ontology{
		RID:         "ri.ontology.main.ontology.abc",
		APIName:     "test",
		DisplayName: "Test Ontology",
		Description: "desc",
	}
	data, err := json.Marshal(o)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var m map[string]interface{}
	json.Unmarshal(data, &m)
	if m["apiName"] != "test" {
		t.Errorf("expected apiName 'test', got %v", m["apiName"])
	}
	if m["rid"] != "ri.ontology.main.ontology.abc" {
		t.Errorf("unexpected rid: %v", m["rid"])
	}
}

func TestObjectType_ToWireJSON(t *testing.T) {
	ot := ObjectType{
		RID:         "ri.ontology.main.object-type.123",
		APIName:     "employee",
		DisplayName: "Employee",
		PrimaryKey:  "employeeId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
		Properties: []Property{
			{RID: "ri.ontology.main.property.p1", APIName: "employeeId", BaseType: "integer"},
			{RID: "ri.ontology.main.property.p2", APIName: "fullName", BaseType: "string"},
		},
	}

	data, err := ot.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON failed: %v", err)
	}

	var wire map[string]interface{}
	json.Unmarshal(data, &wire)

	if wire["apiName"] != "employee" {
		t.Errorf("expected apiName 'employee', got %v", wire["apiName"])
	}
	if wire["primaryKey"] != "employeeId" {
		t.Errorf("expected primaryKey 'employeeId', got %v", wire["primaryKey"])
	}

	props, ok := wire["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, ok := props["employeeId"]; !ok {
		t.Error("expected employeeId property")
	}
	if _, ok := props["fullName"]; !ok {
		t.Error("expected fullName property")
	}
}

func TestLinkType_ToWireJSON(t *testing.T) {
	lt := LinkType{
		RID:              "ri.ontology.main.link-type.lt1",
		APIName:          "employeeDepartment",
		DisplayName:      "Employee Department",
		SourceObjectType: "employee",
		TargetObjectType: "department",
		Cardinality:      "MANY_TO_ONE",
		IsRequired:       false,
	}

	data, err := lt.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON failed: %v", err)
	}

	var wire map[string]interface{}
	json.Unmarshal(data, &wire)

	if wire["apiName"] != "employeeDepartment" {
		t.Errorf("expected apiName, got %v", wire["apiName"])
	}
	if wire["linkedObjectTypeApiName"] != "department" {
		t.Errorf("expected linkedObjectTypeApiName 'department', got %v", wire["linkedObjectTypeApiName"])
	}
	if wire["cardinality"] != "MANY_TO_ONE" {
		t.Errorf("expected cardinality, got %v", wire["cardinality"])
	}
}

func TestActionType_ToWireJSON(t *testing.T) {
	at := ActionType{
		RID:         "ri.ontology.main.action-type.at1",
		APIName:     "createEmployee",
		DisplayName: "Create Employee",
		Status:      "ACTIVE",
		Parameters:  json.RawMessage(`[{"id":"name","type":"string","required":true}]`),
	}

	data, err := at.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON failed: %v", err)
	}

	var wire map[string]interface{}
	json.Unmarshal(data, &wire)

	if wire["apiName"] != "createEmployee" {
		t.Errorf("expected apiName, got %v", wire["apiName"])
	}
	if wire["status"] != "ACTIVE" {
		t.Errorf("expected status ACTIVE, got %v", wire["status"])
	}

	// parameters should be Record<ParameterId, ActionParameterV2>
	params, ok := wire["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters to be a map (Record<ParameterId, ActionParameterV2>)")
	}
	nameDef, ok := params["name"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters.name to be a map")
	}
	dataType, ok := nameDef["dataType"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters.name.dataType to be a map")
	}
	if dataType["type"] != "string" {
		t.Errorf("expected dataType.type 'string', got %v", dataType["type"])
	}
	if nameDef["required"] != true {
		t.Errorf("expected required true, got %v", nameDef["required"])
	}
}

func TestActionType_ToWireJSON_MultipleParams(t *testing.T) {
	at := ActionType{
		RID:     "ri.ontology.main.action-type.at2",
		APIName: "transferEmployee",
		Status:  "ACTIVE",
		Parameters: json.RawMessage(`[
			{"id":"employeeId","type":"integer","required":true},
			{"id":"department","type":"string","required":false,"description":"Target department"},
			{"id":"salary","type":"double","required":true}
		]`),
	}

	data, err := at.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON failed: %v", err)
	}

	var wire map[string]interface{}
	json.Unmarshal(data, &wire)

	params, ok := wire["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters to be a map")
	}

	// Check employeeId
	empDef := params["employeeId"].(map[string]interface{})
	empDT := empDef["dataType"].(map[string]interface{})
	if empDT["type"] != "integer" {
		t.Errorf("expected employeeId dataType.type 'integer', got %v", empDT["type"])
	}
	if empDef["required"] != true {
		t.Errorf("expected employeeId required true")
	}

	// Check department (optional, has description)
	deptDef := params["department"].(map[string]interface{})
	deptDT := deptDef["dataType"].(map[string]interface{})
	if deptDT["type"] != "string" {
		t.Errorf("expected department dataType.type 'string', got %v", deptDT["type"])
	}
	if deptDef["required"] != false {
		t.Errorf("expected department required false")
	}
	if deptDef["description"] != "Target department" {
		t.Errorf("expected department description, got %v", deptDef["description"])
	}

	// Check salary
	salDef := params["salary"].(map[string]interface{})
	salDT := salDef["dataType"].(map[string]interface{})
	if salDT["type"] != "double" {
		t.Errorf("expected salary dataType.type 'double', got %v", salDT["type"])
	}
}

func TestActionType_ToWireJSON_EmptyParams(t *testing.T) {
	at := ActionType{
		RID:        "ri.ontology.main.action-type.at3",
		APIName:    "noParamsAction",
		Status:     "ACTIVE",
		Parameters: json.RawMessage(`[]`),
	}

	data, err := at.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON failed: %v", err)
	}

	var wire map[string]interface{}
	json.Unmarshal(data, &wire)

	params, ok := wire["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters to be an empty map for empty array input")
	}
	if len(params) != 0 {
		t.Errorf("expected empty parameters map, got %d entries", len(params))
	}
}

func TestActionType_ToWireJSON_NullParams(t *testing.T) {
	at := ActionType{
		RID:        "ri.ontology.main.action-type.at4",
		APIName:    "nullParamsAction",
		Status:     "ACTIVE",
		Parameters: json.RawMessage(`null`),
	}

	data, err := at.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON failed: %v", err)
	}

	var wire map[string]interface{}
	json.Unmarshal(data, &wire)

	params, ok := wire["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters to be an empty map for null input")
	}
	if len(params) != 0 {
		t.Errorf("expected empty parameters map, got %d entries", len(params))
	}
}

func TestObjectType_ToWireJSON_ArrayProperty(t *testing.T) {
	ot := ObjectType{
		RID:        "ri.ontology.main.object-type.123",
		APIName:    "employee",
		DisplayName: "Employee",
		PrimaryKey: "id",
		Status:     "ACTIVE",
		Visibility: "NORMAL",
		Properties: []Property{
			{RID: "ri.ontology.main.property.p1", APIName: "tags", BaseType: "string", IsArray: true},
		},
	}

	data, err := ot.ToWireJSON()
	if err != nil {
		t.Fatalf("ToWireJSON failed: %v", err)
	}

	var wire map[string]interface{}
	json.Unmarshal(data, &wire)

	props := wire["properties"].(map[string]interface{})
	tagsProp := props["tags"].(map[string]interface{})
	dataType := tagsProp["dataType"].(map[string]interface{})

	if dataType["type"] != "array" {
		t.Errorf("expected type 'array', got %v", dataType["type"])
	}
	subType := dataType["subType"].(map[string]interface{})
	if subType["type"] != "string" {
		t.Errorf("expected subType 'string', got %v", subType["type"])
	}
}
