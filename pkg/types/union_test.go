package types

import (
	"encoding/json"
	"testing"
)

func TestBaseType_Union_Included(t *testing.T) {
	if !Union.IsValid() {
		t.Fatalf("Union should be a valid BaseType")
	}
	if Union != "union" {
		t.Fatalf("Union wire value must be \"union\", got %q", Union)
	}
}

func TestDataTypeJSON_UnionType_RoundTrip(t *testing.T) {
	dt := DataType{
		Type: Union,
		Variants: []DataType{
			{Type: String},
			{Type: Integer},
		},
	}
	data, err := json.Marshal(dt)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded DataType
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Type != Union {
		t.Fatalf("expected Union, got %s", decoded.Type)
	}
	if len(decoded.Variants) != 2 {
		t.Fatalf("expected 2 variants, got %d", len(decoded.Variants))
	}
	if decoded.Variants[0].Type != String || decoded.Variants[1].Type != Integer {
		t.Fatalf("variants did not round-trip: %+v", decoded.Variants)
	}
}

func TestValidate_Union_MatchesFirstVariant(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: String}, {Type: Integer}}}
	if err := Validate("hello", dt, false); err != nil {
		t.Fatalf("expected match on string variant, got %v", err)
	}
}

func TestValidate_Union_MatchesLaterVariant(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: String}, {Type: Integer}}}
	if err := Validate(42, dt, false); err != nil {
		t.Fatalf("expected match on integer variant, got %v", err)
	}
}

func TestValidate_Union_NoVariantMatches(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: String}, {Type: Integer}}}
	if err := Validate(true, dt, false); err == nil {
		t.Fatal("expected error when no variant matches")
	}
}

func TestValidate_Union_EmptyVariants(t *testing.T) {
	dt := DataType{Type: Union}
	if err := Validate("anything", dt, false); err == nil {
		t.Fatal("expected error for union with zero variants")
	}
}

func TestValidate_Union_NilNullable(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: String}}}
	if err := Validate(nil, dt, true); err != nil {
		t.Fatalf("expected nullable union to accept nil, got %v", err)
	}
}

func TestValidate_Union_DiscriminatorRoutes(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{
		{Type: String},
		{Type: Struct, Fields: map[string]DataType{"address": {Type: String}}},
	}}
	// Value tagged as struct via __type → validated against struct variant.
	value := map[string]interface{}{
		"__type":  "struct",
		"address": "a@b.com",
	}
	if err := Validate(value, dt, false); err != nil {
		t.Fatalf("expected struct variant match via __type, got %v", err)
	}
}

func TestValidate_Union_DiscriminatorUnknownVariant(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: String}, {Type: Integer}}}
	value := map[string]interface{}{"__type": "boolean", "value": true}
	if err := Validate(value, dt, false); err == nil {
		t.Fatal("expected error when __type names an unknown variant")
	}
}

func TestValidate_Union_DiscriminatorMismatchedValue(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: String}, {Type: Integer}}}
	value := map[string]interface{}{"__type": "integer", "value": "not-a-number"}
	if err := Validate(value, dt, false); err == nil {
		t.Fatal("expected error when __type selects a variant the inner value does not match")
	}
}

func TestValidate_Union_WrappedValueMatches(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: String}, {Type: Integer}}}
	value := map[string]interface{}{"__type": "integer", "value": 42}
	if err := Validate(value, dt, false); err != nil {
		t.Fatalf("expected wrapped integer to validate, got %v", err)
	}
}

func TestCoerce_Union_FirstMatchingVariantWins(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: Integer}, {Type: String}}}
	got, err := Coerce("42", dt)
	if err != nil {
		t.Fatalf("coerce: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected wrapped map, got %T", got)
	}
	if m["__type"] != "integer" {
		t.Fatalf("expected integer variant, got %v", m["__type"])
	}
	if m["value"].(int32) != 42 {
		t.Fatalf("expected value 42, got %v", m["value"])
	}
}

func TestCoerce_Union_FallsThroughToLaterVariant(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: Integer}, {Type: String}}}
	got, err := Coerce("hello", dt)
	if err != nil {
		t.Fatalf("coerce: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected wrapped map, got %T", got)
	}
	if m["__type"] != "string" {
		t.Fatalf("expected string variant, got %v", m["__type"])
	}
	if m["value"].(string) != "hello" {
		t.Fatalf("expected value hello, got %v", m["value"])
	}
}

func TestCoerce_Union_HonorsDiscriminatorHint(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: Integer}, {Type: String}}}
	input := map[string]interface{}{"__type": "string", "value": 42}
	got, err := Coerce(input, dt)
	if err != nil {
		t.Fatalf("coerce: %v", err)
	}
	m, ok := got.(map[string]interface{})
	if !ok {
		t.Fatalf("expected wrapped map, got %T", got)
	}
	if m["__type"] != "string" {
		t.Fatalf("expected hint honored to string, got %v", m["__type"])
	}
	if m["value"].(string) != "42" {
		t.Fatalf("expected coerced \"42\" string, got %v", m["value"])
	}
}

func TestCoerce_Union_EmptyVariants(t *testing.T) {
	dt := DataType{Type: Union}
	if _, err := Coerce("x", dt); err == nil {
		t.Fatal("expected error on empty variants")
	}
}

func TestCoerce_Union_NilPassThrough(t *testing.T) {
	dt := DataType{Type: Union, Variants: []DataType{{Type: String}}}
	got, err := Coerce(nil, dt)
	if err != nil {
		t.Fatalf("coerce nil: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil pass-through, got %v", got)
	}
}
