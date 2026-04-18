package oms_test

import (
	"encoding/json"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-215: Function model carries Signature (JSON schema describing params +
// returns) and Runtime ("goja" or "http"). These tests pin the model contract
// so callers (admin handlers, executor) and the wire shape stay in lock-step.

func TestFunction_Validate_RequiresName(t *testing.T) {
	fn := oms.Function{RID: "ri.ontology.main.function.f1", SourceCode: "return 1"}
	if err := fn.Validate(); err == nil {
		t.Fatal("expected error when name is empty")
	}
}

func TestFunction_Validate_RequiresSourceCode(t *testing.T) {
	fn := oms.Function{RID: "ri.ontology.main.function.f1", Name: "f"}
	if err := fn.Validate(); err == nil {
		t.Fatal("expected error when sourceCode is empty")
	}
}

func TestFunction_Validate_DefaultRuntimeIsGoja(t *testing.T) {
	fn := oms.Function{
		RID:        "ri.ontology.main.function.f1",
		Name:       "f",
		SourceCode: "return 1",
	}
	if err := fn.Validate(); err != nil {
		t.Fatalf("expected validation to accept default (empty) runtime, got %v", err)
	}
	if got := fn.NormalisedRuntime(); got != oms.FunctionRuntimeGoja {
		t.Fatalf("expected default runtime=goja, got %q", got)
	}
}

func TestFunction_Validate_AcceptsHTTPRuntime(t *testing.T) {
	fn := oms.Function{
		RID:        "ri.ontology.main.function.f1",
		Name:       "f",
		SourceCode: "https://example.com/run",
		Runtime:    "http",
	}
	if err := fn.Validate(); err != nil {
		t.Fatalf("expected validation to accept runtime=http, got %v", err)
	}
}

func TestFunction_Validate_RejectsUnknownRuntime(t *testing.T) {
	fn := oms.Function{
		RID:        "ri.ontology.main.function.f1",
		Name:       "f",
		SourceCode: "return 1",
		Runtime:    "wasm",
	}
	if err := fn.Validate(); err == nil {
		t.Fatal("expected error for unknown runtime")
	}
}

func TestFunctionSignature_Validate_AcceptsEmpty(t *testing.T) {
	if err := oms.ValidateFunctionSignature(nil); err != nil {
		t.Fatalf("nil signature should validate, got %v", err)
	}
	if err := oms.ValidateFunctionSignature(json.RawMessage("{}")); err != nil {
		t.Fatalf("{} signature should validate, got %v", err)
	}
}

func TestFunctionSignature_Validate_AcceptsValidShape(t *testing.T) {
	sig := json.RawMessage(`{
        "params": [
            {"name":"x","type":"integer","required":true},
            {"name":"y","type":"string","required":false,"default":"hi"}
        ],
        "returns": {"type":"integer"}
    }`)
	if err := oms.ValidateFunctionSignature(sig); err != nil {
		t.Fatalf("expected valid signature, got %v", err)
	}
}

func TestFunctionSignature_Validate_RejectsParamsNotArray(t *testing.T) {
	sig := json.RawMessage(`{"params": {"x": 1}, "returns": {"type":"integer"}}`)
	if err := oms.ValidateFunctionSignature(sig); err == nil {
		t.Fatal("expected error when params is not an array")
	}
}

func TestFunctionSignature_Validate_RejectsParamWithoutName(t *testing.T) {
	sig := json.RawMessage(`{"params": [{"type":"integer"}], "returns": {"type":"integer"}}`)
	if err := oms.ValidateFunctionSignature(sig); err == nil {
		t.Fatal("expected error when a param has no name")
	}
}

func TestFunctionSignature_Validate_RejectsReturnsWithoutType(t *testing.T) {
	sig := json.RawMessage(`{"params": [], "returns": {}}`)
	if err := oms.ValidateFunctionSignature(sig); err == nil {
		t.Fatal("expected error when returns has no type")
	}
}

func TestFunctionSignature_Validate_RejectsInvalidJSON(t *testing.T) {
	sig := json.RawMessage(`{"params":[`)
	if err := oms.ValidateFunctionSignature(sig); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
