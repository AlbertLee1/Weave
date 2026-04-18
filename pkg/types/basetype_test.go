package types

import (
	"encoding/json"
	"testing"
)

func TestBaseType_AllDefined(t *testing.T) {
	all := []BaseType{
		String, Integer, Short, Long, Float, Double, Boolean, Byte,
		Date, Timestamp, Decimal, Array, Struct, Vector, Geopoint,
		Geoshape, Attachment, TimeSeries, MediaReference, Media, Marking, Cipher,
		Union,
	}
	if len(all) != 23 {
		t.Fatalf("expected 23 base types, got %d", len(all))
	}
	seen := make(map[BaseType]bool)
	for _, bt := range all {
		if bt == "" {
			t.Fatal("found empty base type constant")
		}
		if seen[bt] {
			t.Fatalf("duplicate base type: %s", bt)
		}
		seen[bt] = true
	}
}

func TestBaseType_CanBePrimaryKey(t *testing.T) {
	trueCases := []BaseType{String, Integer, Long}
	for _, bt := range trueCases {
		if !bt.CanBePrimaryKey() {
			t.Errorf("expected %s.CanBePrimaryKey() == true", bt)
		}
	}
	falseCases := []BaseType{Array, Struct}
	for _, bt := range falseCases {
		if bt.CanBePrimaryKey() {
			t.Errorf("expected %s.CanBePrimaryKey() == false", bt)
		}
	}
}

func TestDataTypeJSON_SimpleType(t *testing.T) {
	dt := DataType{Type: String}
	data, err := json.Marshal(dt)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"type":"string"}`
	if string(data) != expected {
		t.Fatalf("expected %s, got %s", expected, string(data))
	}

	var decoded DataType
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != String {
		t.Fatalf("expected type %s, got %s", String, decoded.Type)
	}
}

func TestDataTypeJSON_ArrayType(t *testing.T) {
	dt := DataType{Type: Array, SubType: &DataType{Type: String}}
	data, err := json.Marshal(dt)
	if err != nil {
		t.Fatal(err)
	}
	expected := `{"type":"array","subType":{"type":"string"}}`
	if string(data) != expected {
		t.Fatalf("expected %s, got %s", expected, string(data))
	}

	var decoded DataType
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != Array {
		t.Fatalf("expected type %s, got %s", Array, decoded.Type)
	}
	if decoded.SubType == nil || decoded.SubType.Type != String {
		t.Fatal("expected subType to be string")
	}
}

func TestDataTypeJSON_StructType(t *testing.T) {
	dt := DataType{
		Type: Struct,
		Fields: map[string]DataType{
			"name": {Type: String},
			"age":  {Type: Integer},
		},
	}
	data, err := json.Marshal(dt)
	if err != nil {
		t.Fatal(err)
	}

	var decoded DataType
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != Struct {
		t.Fatalf("expected type %s, got %s", Struct, decoded.Type)
	}
	if len(decoded.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(decoded.Fields))
	}
	if decoded.Fields["name"].Type != String {
		t.Fatal("expected field 'name' to be string")
	}
	if decoded.Fields["age"].Type != Integer {
		t.Fatal("expected field 'age' to be integer")
	}
}

func TestDataTypeJSON_DecimalType(t *testing.T) {
	precision := 10
	scale := 2
	dt := DataType{Type: Decimal, Precision: &precision, Scale: &scale}
	data, err := json.Marshal(dt)
	if err != nil {
		t.Fatal(err)
	}

	var decoded DataType
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != Decimal {
		t.Fatalf("expected type %s, got %s", Decimal, decoded.Type)
	}
	if decoded.Precision == nil || *decoded.Precision != 10 {
		t.Fatal("expected precision 10")
	}
	if decoded.Scale == nil || *decoded.Scale != 2 {
		t.Fatal("expected scale 2")
	}
}

func TestBaseType_CanBeTitle(t *testing.T) {
	trueCases := []BaseType{String, Integer, Long, Boolean}
	for _, bt := range trueCases {
		if !bt.CanBeTitle() {
			t.Errorf("expected %s.CanBeTitle() == true", bt)
		}
	}
	falseCases := []BaseType{Struct}
	for _, bt := range falseCases {
		if bt.CanBeTitle() {
			t.Errorf("expected %s.CanBeTitle() == false", bt)
		}
	}
}

func TestBaseType_IsValid(t *testing.T) {
	if !BaseType("string").IsValid() {
		t.Error("expected 'string' to be valid")
	}
	if BaseType("nonexistent").IsValid() {
		t.Error("expected 'nonexistent' to be invalid")
	}
}
