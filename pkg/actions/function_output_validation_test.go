package actions

import (
	"testing"

	"github.com/liyang/weave/pkg/apierror"
)

// TestFunctionOutputValidation verifies that ValidateFunctionOutput correctly
// validates the expected {edits: Edit[]} shape and rejects malformed output
// with InvalidFunctionOutput error codes.

func TestFunctionOutputValidation_ValidResponse(t *testing.T) {
	resp := &FunctionResponse{
		Edits: []FunctionEdit{
			{Type: "CREATE", ObjectType: "Order", PrimaryKey: "ord-1", Properties: map[string]interface{}{"total": 100}},
			{Type: "MODIFY", ObjectType: "Product", PrimaryKey: "prod-A", Properties: map[string]interface{}{"stock": 95}},
			{Type: "DELETE", ObjectType: "TempRecord", PrimaryKey: "tmp-1"},
		},
	}

	err := ValidateFunctionOutput(resp)
	if err != nil {
		t.Fatalf("expected no error for valid response, got: %v", err)
	}
}

func TestFunctionOutputValidation_EmptyEdits(t *testing.T) {
	// Empty edits is valid — a function may decide no changes are needed.
	resp := &FunctionResponse{Edits: []FunctionEdit{}}
	err := ValidateFunctionOutput(resp)
	if err != nil {
		t.Fatalf("expected no error for empty edits, got: %v", err)
	}
}

func TestFunctionOutputValidation_NilResponse(t *testing.T) {
	err := ValidateFunctionOutput(nil)
	if err == nil {
		t.Fatal("expected error for nil response")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("expected StatusCode=400, got %d", apiErr.StatusCode)
	}
}

func TestFunctionOutputValidation_InvalidEditType(t *testing.T) {
	resp := &FunctionResponse{
		Edits: []FunctionEdit{
			{Type: "UPSERT", ObjectType: "Order", PrimaryKey: "ord-1"},
		},
	}

	err := ValidateFunctionOutput(resp)
	if err == nil {
		t.Fatal("expected error for invalid edit type")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("expected StatusCode=400, got %d", apiErr.StatusCode)
	}
	if apiErr.Parameters["editIndex"] != "0" {
		t.Errorf("expected editIndex=0, got %q", apiErr.Parameters["editIndex"])
	}
	if apiErr.Parameters["field"] != "type" {
		t.Errorf("expected field=type, got %q", apiErr.Parameters["field"])
	}
}

func TestFunctionOutputValidation_EmptyEditType(t *testing.T) {
	resp := &FunctionResponse{
		Edits: []FunctionEdit{
			{Type: "", ObjectType: "Order", PrimaryKey: "ord-1"},
		},
	}

	err := ValidateFunctionOutput(resp)
	if err == nil {
		t.Fatal("expected error for empty edit type")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
}

func TestFunctionOutputValidation_MissingObjectType(t *testing.T) {
	resp := &FunctionResponse{
		Edits: []FunctionEdit{
			{Type: "CREATE", ObjectType: "", PrimaryKey: "ord-1"},
		},
	}

	err := ValidateFunctionOutput(resp)
	if err == nil {
		t.Fatal("expected error for empty objectType")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
	if apiErr.Parameters["field"] != "objectType" {
		t.Errorf("expected field=objectType, got %q", apiErr.Parameters["field"])
	}
}

func TestFunctionOutputValidation_MissingPrimaryKey(t *testing.T) {
	resp := &FunctionResponse{
		Edits: []FunctionEdit{
			{Type: "MODIFY", ObjectType: "Product", PrimaryKey: ""},
		},
	}

	err := ValidateFunctionOutput(resp)
	if err == nil {
		t.Fatal("expected error for empty primaryKey")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
	if apiErr.Parameters["field"] != "primaryKey" {
		t.Errorf("expected field=primaryKey, got %q", apiErr.Parameters["field"])
	}
}

func TestFunctionOutputValidation_MultipleEdits_SecondInvalid(t *testing.T) {
	resp := &FunctionResponse{
		Edits: []FunctionEdit{
			{Type: "CREATE", ObjectType: "Order", PrimaryKey: "ord-1"},
			{Type: "MODIFY", ObjectType: "", PrimaryKey: "prod-A"}, // invalid
		},
	}

	err := ValidateFunctionOutput(resp)
	if err == nil {
		t.Fatal("expected error for second edit with empty objectType")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.Parameters["editIndex"] != "1" {
		t.Errorf("expected editIndex=1, got %q", apiErr.Parameters["editIndex"])
	}
}

// TestFunctionOutputValidation_RawOutput tests validation of raw Goja output
// (map[string]interface{}) that doesn't match the expected shape.
func TestFunctionOutputValidation_RawNotMap(t *testing.T) {
	err := ValidateRawFunctionOutput("just a string")
	if err == nil {
		t.Fatal("expected error for non-map output")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
}

func TestFunctionOutputValidation_RawMissingEditsKey(t *testing.T) {
	raw := map[string]interface{}{
		"result": "success",
	}
	err := ValidateRawFunctionOutput(raw)
	if err == nil {
		t.Fatal("expected error for missing edits key")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
}

func TestFunctionOutputValidation_RawEditsNotArray(t *testing.T) {
	raw := map[string]interface{}{
		"edits": "not an array",
	}
	err := ValidateRawFunctionOutput(raw)
	if err == nil {
		t.Fatal("expected error for edits not being an array")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
}

func TestFunctionOutputValidation_RawEditNotObject(t *testing.T) {
	raw := map[string]interface{}{
		"edits": []interface{}{"not an object"},
	}
	err := ValidateRawFunctionOutput(raw)
	if err == nil {
		t.Fatal("expected error for edit not being an object")
	}
	apiErr, ok := err.(*apierror.APIError)
	if !ok {
		t.Fatalf("expected *apierror.APIError, got %T", err)
	}
	if apiErr.ErrorName != "InvalidFunctionOutput" {
		t.Errorf("expected ErrorName=InvalidFunctionOutput, got %q", apiErr.ErrorName)
	}
}

func TestFunctionOutputValidation_RawValidOutput(t *testing.T) {
	raw := map[string]interface{}{
		"edits": []interface{}{
			map[string]interface{}{
				"type":       "CREATE",
				"objectType": "Order",
				"primaryKey": "ord-1",
				"properties": map[string]interface{}{"total": 100},
			},
		},
	}
	err := ValidateRawFunctionOutput(raw)
	if err != nil {
		t.Fatalf("expected no error for valid raw output, got: %v", err)
	}
}
