package actions

import (
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
)

// TestFunctionRequest_SerializesCorrectly verifies the JSON envelope sent to
// the function carries the action type identifiers and the parameter map. The
// shape is the wire contract that function authors program against.
func TestFunctionRequest_SerializesCorrectly(t *testing.T) {
	req := FunctionRequest{
		ActionTypeRID: "ri.ontology.main.action-type.create-employee",
		ActionTypeAPI: "createEmployee",
		FunctionRID:   "ri.functions.main.function.create-employee-fn",
		Parameters: map[string]interface{}{
			"name": "Alice",
			"age":  float64(30),
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var round map[string]interface{}
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if round["actionTypeRid"] != "ri.ontology.main.action-type.create-employee" {
		t.Errorf("actionTypeRid: got %v", round["actionTypeRid"])
	}
	if round["actionTypeApiName"] != "createEmployee" {
		t.Errorf("actionTypeApiName: got %v", round["actionTypeApiName"])
	}
	if round["functionRid"] != "ri.functions.main.function.create-employee-fn" {
		t.Errorf("functionRid: got %v", round["functionRid"])
	}
	params, ok := round["parameters"].(map[string]interface{})
	if !ok {
		t.Fatalf("parameters not a map: %T", round["parameters"])
	}
	if params["name"] != "Alice" {
		t.Errorf("parameters.name: got %v", params["name"])
	}
	if params["age"].(float64) != 30 {
		t.Errorf("parameters.age: got %v", params["age"])
	}
}

// TestFunctionEdit_ToFunnelEdit_Create verifies CREATE conversion.
func TestFunctionEdit_ToFunnelEdit_Create(t *testing.T) {
	fe := FunctionEdit{
		Type:       "CREATE",
		ObjectType: "Employee",
		PrimaryKey: "emp-1",
		Properties: map[string]interface{}{
			"name":   "Alice",
			"salary": float64(100000),
		},
	}

	edit, err := fe.ToFunnelEdit()
	if err != nil {
		t.Fatalf("ToFunnelEdit: %v", err)
	}
	if edit.Type != funnel.EditTypeCreate {
		t.Errorf("Type: got %s, want CREATE", edit.Type)
	}
	if edit.ObjectType != "Employee" {
		t.Errorf("ObjectType: got %s", edit.ObjectType)
	}
	if edit.PrimaryKey != "emp-1" {
		t.Errorf("PrimaryKey: got %s", edit.PrimaryKey)
	}
	if edit.Properties["name"] != "Alice" {
		t.Errorf("Properties.name: got %v", edit.Properties["name"])
	}
	if edit.Properties["salary"].(float64) != 100000 {
		t.Errorf("Properties.salary: got %v", edit.Properties["salary"])
	}
}

// TestFunctionEdit_ToFunnelEdit_Modify verifies MODIFY conversion.
func TestFunctionEdit_ToFunnelEdit_Modify(t *testing.T) {
	fe := FunctionEdit{
		Type:       "MODIFY",
		ObjectType: "Employee",
		PrimaryKey: "emp-2",
		Properties: map[string]interface{}{
			"salary": float64(120000),
		},
	}

	edit, err := fe.ToFunnelEdit()
	if err != nil {
		t.Fatalf("ToFunnelEdit: %v", err)
	}
	if edit.Type != funnel.EditTypeModify {
		t.Errorf("Type: got %s, want MODIFY", edit.Type)
	}
	if edit.PrimaryKey != "emp-2" {
		t.Errorf("PrimaryKey: got %s", edit.PrimaryKey)
	}
	if edit.Properties["salary"].(float64) != 120000 {
		t.Errorf("Properties.salary: got %v", edit.Properties["salary"])
	}
}

// TestFunctionEdit_ToFunnelEdit_Delete verifies DELETE conversion (no props).
func TestFunctionEdit_ToFunnelEdit_Delete(t *testing.T) {
	fe := FunctionEdit{
		Type:       "DELETE",
		ObjectType: "Employee",
		PrimaryKey: "emp-3",
	}

	edit, err := fe.ToFunnelEdit()
	if err != nil {
		t.Fatalf("ToFunnelEdit: %v", err)
	}
	if edit.Type != funnel.EditTypeDelete {
		t.Errorf("Type: got %s, want DELETE", edit.Type)
	}
	if edit.PrimaryKey != "emp-3" {
		t.Errorf("PrimaryKey: got %s", edit.PrimaryKey)
	}
	if len(edit.Properties) != 0 {
		t.Errorf("Properties should be empty for DELETE, got %v", edit.Properties)
	}
}

// TestFunctionEdit_ToFunnelEdit_UnknownType verifies an unknown edit type
// returns an error so a misbehaving function does not silently produce
// no-op edits downstream.
func TestFunctionEdit_ToFunnelEdit_UnknownType(t *testing.T) {
	fe := FunctionEdit{Type: "INVALID", ObjectType: "X", PrimaryKey: "1"}
	if _, err := fe.ToFunnelEdit(); err == nil {
		t.Fatal("expected error for unknown type")
	}
}
