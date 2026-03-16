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

	// parameters should be present
	if wire["parameters"] == nil {
		t.Error("expected parameters to be present")
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
