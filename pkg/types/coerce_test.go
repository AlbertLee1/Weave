package types

import (
	"testing"
	"time"
)

func TestCoerce_FloatToInteger(t *testing.T) {
	result, err := Coerce(float64(42.0), DataType{Type: Integer})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	v, ok := result.(int32)
	if !ok {
		t.Fatalf("expected int32, got %T", result)
	}
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}
}

func TestCoerce_FloatToLong(t *testing.T) {
	result, err := Coerce(float64(100.0), DataType{Type: Long})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	v, ok := result.(int64)
	if !ok {
		t.Fatalf("expected int64, got %T", result)
	}
	if v != 100 {
		t.Fatalf("expected 100, got %d", v)
	}
}

func TestCoerce_StringToTimestamp(t *testing.T) {
	result, err := Coerce("2024-01-15T10:30:00Z", DataType{Type: Timestamp})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	v, ok := result.(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T", result)
	}
	expected := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !v.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, v)
	}
}

func TestCoerce_StringToDate(t *testing.T) {
	result, err := Coerce("2024-01-15", DataType{Type: Date})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	v, ok := result.(time.Time)
	if !ok {
		t.Fatalf("expected time.Time, got %T", result)
	}
	expected := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	if !v.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, v)
	}
}

func TestCoerce_IntegerToFloat(t *testing.T) {
	result, err := Coerce(int32(42), DataType{Type: Float})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	v, ok := result.(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", result)
	}
	if v != 42.0 {
		t.Fatalf("expected 42.0, got %f", v)
	}
}

func TestCoerce_InvalidCoercion(t *testing.T) {
	_, err := Coerce("abc", DataType{Type: Integer})
	if err == nil {
		t.Fatal("expected error for invalid coercion")
	}
}

func TestCoerce_ArrayOfStrings(t *testing.T) {
	dt := DataType{Type: Array, SubType: &DataType{Type: String}}
	result, err := Coerce([]interface{}{"a", "b"}, dt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	arr, ok := result.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", result)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(arr))
	}
}

func TestCoerce_NestedStruct(t *testing.T) {
	dt := DataType{
		Type: Struct,
		Fields: map[string]DataType{
			"name": {Type: String},
			"age":  {Type: Integer},
		},
	}
	input := map[string]interface{}{
		"name": "Alice",
		"age":  float64(30),
	}
	result, err := Coerce(input, dt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	m, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}
	if m["name"] != "Alice" {
		t.Fatalf("expected name Alice, got %v", m["name"])
	}
	age, ok := m["age"].(int32)
	if !ok {
		t.Fatalf("expected age to be int32, got %T", m["age"])
	}
	if age != 30 {
		t.Fatalf("expected age 30, got %d", age)
	}
}
