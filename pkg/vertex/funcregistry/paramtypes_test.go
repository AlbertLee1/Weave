package funcregistry_test

import (
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/funcregistry"
)

func TestIsAllowedParameterType_Given_PrimitiveScalar_When_Check_Then_True(t *testing.T) {
	for _, p := range []string{"string", "integer", "short", "long", "float", "double", "boolean", "byte", "date", "timestamp", "decimal"} {
		if !funcregistry.IsAllowedParameterType(p) {
			t.Errorf("%q expected to be an allowed parameter type", p)
		}
	}
}

func TestIsAllowedParameterType_Given_Collection_When_Check_Then_True(t *testing.T) {
	if !funcregistry.IsAllowedParameterType("array") {
		t.Fatalf("array should be allowed (Collection)")
	}
}

func TestIsAllowedParameterType_Given_EmptyString_When_Check_Then_True(t *testing.T) {
	if !funcregistry.IsAllowedParameterType("") {
		t.Fatalf("empty type (untyped param) should be allowed")
	}
}

func TestIsAllowedParameterType_Given_AggregationOrNotification_When_Check_Then_False(t *testing.T) {
	for _, p := range []string{"aggregation", "Aggregation", "notification", "Notification"} {
		if funcregistry.IsAllowedParameterType(p) {
			t.Errorf("%q expected to be rejected", p)
		}
	}
}

func TestIsAllowedParameterType_Given_UnknownType_When_Check_Then_False(t *testing.T) {
	for _, p := range []string{"struct", "vector", "geopoint", "marking", "media", "mediaReference", "timeseries", "cipher", "union", "attachment", "garbage"} {
		if funcregistry.IsAllowedParameterType(p) {
			t.Errorf("%q expected to be rejected (only primitive scalars + array are allowed)", p)
		}
	}
}

func TestValidateParameterTypes_Given_EmptySignature_When_Validate_Then_Pass(t *testing.T) {
	if err := funcregistry.ValidateParameterTypes(oms.ParsedFunctionSignature{}); err != nil {
		t.Fatalf("empty signature should pass: %v", err)
	}
}

func TestValidateParameterTypes_Given_AllPrimitiveParams_When_Validate_Then_Pass(t *testing.T) {
	sig := oms.ParsedFunctionSignature{
		Params: []oms.FunctionParam{
			{Name: "a", Type: "integer", Required: true},
			{Name: "b", Type: "string"},
		},
		Returns: &oms.FunctionReturn{Type: "double"},
	}
	if err := funcregistry.ValidateParameterTypes(sig); err != nil {
		t.Fatalf("primitive-only signature should pass: %v", err)
	}
}

func TestValidateParameterTypes_Given_AggregationParam_When_Validate_Then_TypedErrorRejected(t *testing.T) {
	sig := oms.ParsedFunctionSignature{
		Params: []oms.FunctionParam{
			{Name: "input", Type: "aggregation", Required: true},
		},
	}
	err := funcregistry.ValidateParameterTypes(sig)
	if err == nil {
		t.Fatalf("expected error for aggregation param")
	}
	var pe *funcregistry.ParamTypeError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParamTypeError, got %T (%v)", err, err)
	}
	if pe.Parameter != "input" {
		t.Errorf("Parameter = %q, want %q", pe.Parameter, "input")
	}
	if pe.Type != "aggregation" {
		t.Errorf("Type = %q, want %q", pe.Type, "aggregation")
	}
}

func TestValidateParameterTypes_Given_NotificationReturn_When_Validate_Then_TypedErrorRejected(t *testing.T) {
	sig := oms.ParsedFunctionSignature{
		Returns: &oms.FunctionReturn{Type: "notification"},
	}
	err := funcregistry.ValidateParameterTypes(sig)
	if err == nil {
		t.Fatalf("expected error for notification return")
	}
	var pe *funcregistry.ParamTypeError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ParamTypeError, got %T (%v)", err, err)
	}
	if pe.Parameter != "" {
		t.Errorf("Parameter for return should be empty, got %q", pe.Parameter)
	}
	if pe.Type != "notification" {
		t.Errorf("Type = %q, want %q", pe.Type, "notification")
	}
	if pe.IsReturn != true {
		t.Errorf("IsReturn = %v, want true", pe.IsReturn)
	}
}

func TestValidateParameterTypes_Given_ArrayParam_When_Validate_Then_Pass(t *testing.T) {
	sig := oms.ParsedFunctionSignature{
		Params: []oms.FunctionParam{
			{Name: "items", Type: "array", Required: true},
		},
	}
	if err := funcregistry.ValidateParameterTypes(sig); err != nil {
		t.Fatalf("array param (Collection) should pass: %v", err)
	}
}

func TestParamTypeError_Given_ParamFailure_When_Error_Then_NamedTypedMessage(t *testing.T) {
	pe := &funcregistry.ParamTypeError{Parameter: "x", Type: "aggregation"}
	msg := pe.Error()
	if msg == "" {
		t.Fatalf("ParamTypeError.Error() should not be empty")
	}
	// Spot-check key tokens are present so consumers can surface them.
	for _, want := range []string{"x", "aggregation"} {
		if !contains(msg, want) {
			t.Errorf("Error() = %q, want substring %q", msg, want)
		}
	}
}

func TestParamTypeError_Given_ReturnFailure_When_Error_Then_NamesReturn(t *testing.T) {
	pe := &funcregistry.ParamTypeError{Type: "notification", IsReturn: true}
	msg := pe.Error()
	if !contains(msg, "return") {
		t.Errorf("Error() = %q, want substring 'return'", msg)
	}
	if !contains(msg, "notification") {
		t.Errorf("Error() = %q, want substring 'notification'", msg)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
