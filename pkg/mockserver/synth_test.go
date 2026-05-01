package mockserver

import (
	"reflect"
	"testing"
)

func TestSynthesize_String(t *testing.T) {
	s := newSynthesizer(map[string]any{}, map[string]any{})
	got := s.synthesize(map[string]any{"type": "string"})
	if got != "" {
		t.Errorf("default string = %v, want empty string", got)
	}
}

func TestSynthesize_StringPrefersExample(t *testing.T) {
	s := newSynthesizer(map[string]any{}, map[string]any{})
	got := s.synthesize(map[string]any{"type": "string", "example": "alice"})
	if got != "alice" {
		t.Errorf("example string = %v, want alice", got)
	}
}

func TestSynthesize_StringPrefersEnumWhenNoExample(t *testing.T) {
	s := newSynthesizer(map[string]any{}, map[string]any{})
	got := s.synthesize(map[string]any{"type": "string", "enum": []any{"PROMINENT", "NORMAL"}})
	if got != "PROMINENT" {
		t.Errorf("enum string = %v, want PROMINENT", got)
	}
}

func TestSynthesize_IntegerNumber(t *testing.T) {
	s := newSynthesizer(map[string]any{}, map[string]any{})
	if got := s.synthesize(map[string]any{"type": "integer"}); got != int64(0) {
		t.Errorf("integer = %v, want 0", got)
	}
	if got := s.synthesize(map[string]any{"type": "number"}); got != float64(0) {
		t.Errorf("number = %v, want 0.0", got)
	}
	if got := s.synthesize(map[string]any{"type": "boolean"}); got != false {
		t.Errorf("boolean = %v, want false", got)
	}
}

func TestSynthesize_Object(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":     map[string]any{"type": "string", "example": "abc"},
			"count":  map[string]any{"type": "integer"},
			"active": map[string]any{"type": "boolean"},
		},
	}
	s := newSynthesizer(map[string]any{}, map[string]any{})
	got, ok := s.synthesize(schema).(map[string]any)
	if !ok {
		t.Fatalf("synthesize did not return a map: %#v", got)
	}
	if got["id"] != "abc" {
		t.Errorf("id = %v", got["id"])
	}
	if got["count"] != int64(0) {
		t.Errorf("count = %v", got["count"])
	}
	if got["active"] != false {
		t.Errorf("active = %v", got["active"])
	}
}

func TestSynthesize_ArrayProducesOneElement(t *testing.T) {
	schema := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string", "example": "x"},
	}
	s := newSynthesizer(map[string]any{}, map[string]any{})
	got, ok := s.synthesize(schema).([]any)
	if !ok {
		t.Fatalf("not an array: %#v", got)
	}
	if !reflect.DeepEqual(got, []any{"x"}) {
		t.Errorf("array = %#v", got)
	}
}

func TestSynthesize_ResolvesRef(t *testing.T) {
	schemas := map[string]any{
		"Ontology": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"apiName": map[string]any{"type": "string", "example": "northwind"},
			},
		},
	}
	s := newSynthesizer(schemas, map[string]any{})
	got, ok := s.synthesize(map[string]any{"$ref": "#/components/schemas/Ontology"}).(map[string]any)
	if !ok {
		t.Fatalf("not a map")
	}
	if got["apiName"] != "northwind" {
		t.Errorf("apiName = %v", got["apiName"])
	}
}

func TestSynthesize_HandlesRefCycleSafely(t *testing.T) {
	schemas := map[string]any{
		"Node": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":  map[string]any{"type": "string"},
				"child": map[string]any{"$ref": "#/components/schemas/Node"},
			},
		},
	}
	s := newSynthesizer(schemas, map[string]any{})
	// Should not blow the stack.
	out, ok := s.synthesize(map[string]any{"$ref": "#/components/schemas/Node"}).(map[string]any)
	if !ok {
		t.Fatalf("not a map")
	}
	if _, ok := out["name"]; !ok {
		t.Error("expected name field")
	}
	// child must be present and either nil or a map (cycle break point).
	if _, present := out["child"]; !present {
		t.Error("expected child field present")
	}
}

func TestSynthesize_AllOfMerges(t *testing.T) {
	schema := map[string]any{
		"allOf": []any{
			map[string]any{"type": "object", "properties": map[string]any{"a": map[string]any{"type": "string", "example": "A"}}},
			map[string]any{"type": "object", "properties": map[string]any{"b": map[string]any{"type": "integer"}}},
		},
	}
	s := newSynthesizer(map[string]any{}, map[string]any{})
	got, ok := s.synthesize(schema).(map[string]any)
	if !ok {
		t.Fatalf("not a map")
	}
	if got["a"] != "A" {
		t.Errorf("a = %v", got["a"])
	}
	if got["b"] != int64(0) {
		t.Errorf("b = %v", got["b"])
	}
}

func TestSynthesize_OneOfPicksFirst(t *testing.T) {
	schema := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string", "example": "first"},
			map[string]any{"type": "integer"},
		},
	}
	s := newSynthesizer(map[string]any{}, map[string]any{})
	got := s.synthesize(schema)
	if got != "first" {
		t.Errorf("oneOf = %v", got)
	}
}
