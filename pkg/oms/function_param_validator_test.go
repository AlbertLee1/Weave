package oms_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// US-216: ValidateAndCoerceFunctionParams enforces the runtime contract for
// every function call site. The tests below pin the contract because handler
// surfaces (HTTP 400 with parameter+code) read Code directly.

func mustParseSignature(t *testing.T, raw string) oms.ParsedFunctionSignature {
	t.Helper()
	sig, err := oms.ParseFunctionSignature(json.RawMessage(raw))
	if err != nil {
		t.Fatalf("parse signature: %v", err)
	}
	return sig
}

func TestParseFunctionSignature_EmptyHasNoContract(t *testing.T) {
	cases := []string{"", "null", "{}", "  {}  "}
	for _, raw := range cases {
		sig, err := oms.ParseFunctionSignature(json.RawMessage(raw))
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		if sig.HasContract() {
			t.Fatalf("%q should not declare a contract", raw)
		}
	}
}

func TestParseFunctionSignature_TypedShape(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [
            {"name":"x","type":"integer","required":true},
            {"name":"y","type":"string","default":"hi"}
        ],
        "returns": {"type":"integer"}
    }`)
	if !sig.HasContract() {
		t.Fatal("expected contract to be declared")
	}
	if len(sig.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(sig.Params))
	}
	if sig.Params[0].Name != "x" || sig.Params[0].Type != "integer" || !sig.Params[0].Required {
		t.Errorf("param[0] mismatch: %+v", sig.Params[0])
	}
	if sig.Params[1].Required {
		t.Errorf("param[1] should be optional")
	}
	if string(sig.Params[1].Default) != `"hi"` {
		t.Errorf("param[1] default = %s, want \"hi\"", sig.Params[1].Default)
	}
	if sig.Returns == nil || sig.Returns.Type != "integer" {
		t.Errorf("returns mismatch: %+v", sig.Returns)
	}
}

func TestValidateAndCoerce_NoContractPassesThrough(t *testing.T) {
	sig := mustParseSignature(t, "")
	in := map[string]interface{}{"anything": 1, "extra": "ok"}
	out, err := oms.ValidateAndCoerceFunctionParams(sig, in)
	if err != nil {
		t.Fatalf("expected pass-through, got %v", err)
	}
	if len(out) != 2 || out["anything"] != 1 || out["extra"] != "ok" {
		t.Fatalf("expected input copied through, got %+v", out)
	}
	out["anything"] = 999
	if in["anything"] == 999 {
		t.Fatal("output should be a fresh allocation, not aliasing input")
	}
}

func TestValidateAndCoerce_HappyPath(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [
            {"name":"a","type":"integer","required":true},
            {"name":"b","type":"string","required":true}
        ],
        "returns":{"type":"integer"}
    }`)
	in := map[string]interface{}{"a": 1, "b": "hello"}
	out, err := oms.ValidateAndCoerceFunctionParams(sig, in)
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if out["a"] != 1 || out["b"] != "hello" {
		t.Fatalf("expected coerced map to mirror input, got %+v", out)
	}
}

func TestValidateAndCoerce_RejectsMissingRequired(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [{"name":"a","type":"integer","required":true}]
    }`)
	_, err := oms.ValidateAndCoerceFunctionParams(sig, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected missing_required error")
	}
	var pe *oms.FunctionParamError
	if !errors.As(err, &pe) {
		t.Fatalf("expected FunctionParamError, got %T", err)
	}
	if pe.Code != "missing_required" || pe.Parameter != "a" {
		t.Errorf("unexpected err: %+v", pe)
	}
}

func TestValidateAndCoerce_RequiredAcceptsZeroValues(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [
            {"name":"n","type":"integer","required":true},
            {"name":"s","type":"string","required":true},
            {"name":"b","type":"boolean","required":true}
        ]
    }`)
	in := map[string]interface{}{"n": 0, "s": "", "b": false}
	out, err := oms.ValidateAndCoerceFunctionParams(sig, in)
	if err != nil {
		t.Fatalf("zero values should pass when present, got %v", err)
	}
	if out["n"] != 0 || out["s"] != "" || out["b"] != false {
		t.Fatalf("zero values lost: %+v", out)
	}
}

func TestValidateAndCoerce_AppliesDefault(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [
            {"name":"limit","type":"integer","default":10},
            {"name":"label","type":"string","default":"all"}
        ]
    }`)
	out, err := oms.ValidateAndCoerceFunctionParams(sig, map[string]interface{}{})
	if err != nil {
		t.Fatalf("expected defaults to apply, got %v", err)
	}
	if out["limit"] != float64(10) {
		t.Errorf("expected limit default 10, got %v (%T)", out["limit"], out["limit"])
	}
	if out["label"] != "all" {
		t.Errorf("expected label default \"all\", got %v", out["label"])
	}
}

func TestValidateAndCoerce_DefaultDoesNotOverrideExplicit(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [{"name":"limit","type":"integer","default":10}]
    }`)
	out, err := oms.ValidateAndCoerceFunctionParams(sig, map[string]interface{}{"limit": 25})
	if err != nil {
		t.Fatal(err)
	}
	if out["limit"] != 25 {
		t.Fatalf("explicit value lost: %+v", out)
	}
}

func TestValidateAndCoerce_RejectsTypeMismatch(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [{"name":"a","type":"integer","required":true}]
    }`)
	_, err := oms.ValidateAndCoerceFunctionParams(sig, map[string]interface{}{"a": "not a number"})
	var pe *oms.FunctionParamError
	if !errors.As(err, &pe) {
		t.Fatalf("expected FunctionParamError, got %v", err)
	}
	if pe.Code != "type_mismatch" || pe.Parameter != "a" {
		t.Errorf("unexpected err: %+v", pe)
	}
}

func TestValidateAndCoerce_RejectsUnknownParameter(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [{"name":"a","type":"integer"}]
    }`)
	_, err := oms.ValidateAndCoerceFunctionParams(sig, map[string]interface{}{"a": 1, "extra": true})
	var pe *oms.FunctionParamError
	if !errors.As(err, &pe) {
		t.Fatalf("expected FunctionParamError, got %v", err)
	}
	if pe.Code != "unknown_parameter" || pe.Parameter != "extra" {
		t.Errorf("unexpected err: %+v", pe)
	}
}

func TestValidateAndCoerce_OptionalAcceptsNil(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [
            {"name":"a","type":"integer","required":true},
            {"name":"b","type":"string"}
        ]
    }`)
	out, err := oms.ValidateAndCoerceFunctionParams(sig, map[string]interface{}{"a": 1, "b": nil})
	if err != nil {
		t.Fatalf("optional nil should be accepted, got %v", err)
	}
	if _, present := out["b"]; present {
		t.Errorf("expected optional-with-nil to be omitted from output, got %+v", out)
	}
}

func TestValidateAndCoerce_BooleanAndArrayAndStruct(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [
            {"name":"flag","type":"boolean","required":true},
            {"name":"items","type":"array","required":true},
            {"name":"obj","type":"struct","required":true}
        ]
    }`)
	in := map[string]interface{}{
		"flag":  true,
		"items": []interface{}{"a", "b"},
		"obj":   map[string]interface{}{"k": "v"},
	}
	out, err := oms.ValidateAndCoerceFunctionParams(sig, in)
	if err != nil {
		t.Fatalf("happy path failed: %v", err)
	}
	if out["flag"] != true {
		t.Error("boolean lost")
	}
	if len(out["items"].([]interface{})) != 2 {
		t.Error("array lost")
	}
	if out["obj"].(map[string]interface{})["k"] != "v" {
		t.Error("struct lost")
	}
}

func TestValidateAndCoerce_ParamWithoutDeclaredTypeAcceptsAny(t *testing.T) {
	// Some callers prefer to skip per-param typing and only enforce
	// presence/required semantics. An empty `type` should accept anything
	// rather than silently rejecting via the types.Validate path.
	sig := mustParseSignature(t, `{
        "params": [{"name":"x","required":true}]
    }`)
	cases := []interface{}{1, "s", true, []interface{}{1}, map[string]interface{}{"k": 1}}
	for _, v := range cases {
		if _, err := oms.ValidateAndCoerceFunctionParams(sig, map[string]interface{}{"x": v}); err != nil {
			t.Fatalf("typeless param should accept %T(%v), got %v", v, v, err)
		}
	}
}

func TestValidateAndCoerce_DefaultWithComplexJSON(t *testing.T) {
	sig := mustParseSignature(t, `{
        "params": [{"name":"opts","type":"struct","default":{"foo":"bar","n":3}}]
    }`)
	out, err := oms.ValidateAndCoerceFunctionParams(sig, map[string]interface{}{})
	if err != nil {
		t.Fatalf("expected struct default applied, got %v", err)
	}
	m, ok := out["opts"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected struct default as map, got %T", out["opts"])
	}
	if m["foo"] != "bar" || m["n"] != float64(3) {
		t.Errorf("default decoded incorrectly: %+v", m)
	}
}
