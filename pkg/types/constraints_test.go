package types

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func constraintsJSON(v interface{}) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// ---------------------------------------------------------------------------
// Regex constraint
// ---------------------------------------------------------------------------

func TestValidateConstraints_Regex_Pass(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"regex": `^[a-z]+@[a-z]+\.[a-z]+$`,
	})
	if err := ValidateConstraints("alice@example.com", c); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestValidateConstraints_Regex_Fail(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"regex": `^[a-z]+@[a-z]+\.[a-z]+$`,
	})
	err := ValidateConstraints("not-an-email", c)
	if err == nil {
		t.Fatal("expected error for regex mismatch")
	}
	if !strings.Contains(err.Error(), "regex") {
		t.Fatalf("error should mention regex, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MinLength / MaxLength constraints
// ---------------------------------------------------------------------------

func TestValidateConstraints_MinLength_Pass(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"minLength": 3,
	})
	if err := ValidateConstraints("hello", c); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestValidateConstraints_MinLength_Fail(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"minLength": 5,
	})
	err := ValidateConstraints("hi", c)
	if err == nil {
		t.Fatal("expected error for minLength violation")
	}
	if !strings.Contains(err.Error(), "minLength") {
		t.Fatalf("error should mention minLength, got: %v", err)
	}
}

func TestValidateConstraints_MaxLength_Pass(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"maxLength": 10,
	})
	if err := ValidateConstraints("hello", c); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestValidateConstraints_MaxLength_Fail(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"maxLength": 3,
	})
	err := ValidateConstraints("hello world", c)
	if err == nil {
		t.Fatal("expected error for maxLength violation")
	}
	if !strings.Contains(err.Error(), "maxLength") {
		t.Fatalf("error should mention maxLength, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Min / Max (range) constraints
// ---------------------------------------------------------------------------

func TestValidateConstraints_Min_Pass(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"min": 0,
	})
	if err := ValidateConstraints(float64(5), c); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestValidateConstraints_Min_Fail(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"min": 10,
	})
	err := ValidateConstraints(float64(5), c)
	if err == nil {
		t.Fatal("expected error for min violation")
	}
	if !strings.Contains(err.Error(), "min") {
		t.Fatalf("error should mention min, got: %v", err)
	}
}

func TestValidateConstraints_Max_Pass(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"max": 100,
	})
	if err := ValidateConstraints(float64(50), c); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestValidateConstraints_Max_Fail(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"max": 10,
	})
	err := ValidateConstraints(float64(50), c)
	if err == nil {
		t.Fatal("expected error for max violation")
	}
	if !strings.Contains(err.Error(), "max") {
		t.Fatalf("error should mention max, got: %v", err)
	}
}

func TestValidateConstraints_MinMax_IntValue(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"min": 1,
		"max": 100,
	})
	// int values should be coerced to float64 for comparison
	if err := ValidateConstraints(50, c); err != nil {
		t.Fatalf("expected pass for int value, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Enum constraint
// ---------------------------------------------------------------------------

func TestValidateConstraints_Enum_Pass(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"enum": []string{"red", "green", "blue"},
	})
	if err := ValidateConstraints("green", c); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestValidateConstraints_Enum_Fail(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"enum": []string{"red", "green", "blue"},
	})
	err := ValidateConstraints("yellow", c)
	if err == nil {
		t.Fatal("expected error for enum violation")
	}
	if !strings.Contains(err.Error(), "enum") {
		t.Fatalf("error should mention enum, got: %v", err)
	}
}

// US-208: enum violations must be a typed *EnumViolationError so HTTP layers
// can build a structured 422 response containing the allowedValues list and
// the rejected value verbatim.
func TestValidateConstraints_Enum_TypedError(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"enum": []string{"low", "medium", "high", "critical"},
	})
	err := ValidateConstraints("urgent", c)
	if err == nil {
		t.Fatal("expected enum violation")
	}
	var ev *EnumViolationError
	if !errors.As(err, &ev) {
		t.Fatalf("expected *EnumViolationError via errors.As, got %T: %v", err, err)
	}
	wantAllowed := []string{"low", "medium", "high", "critical"}
	if !reflect.DeepEqual(ev.AllowedValues, wantAllowed) {
		t.Fatalf("AllowedValues = %v, want %v", ev.AllowedValues, wantAllowed)
	}
	if got := reflect.ValueOf(ev.Value).String(); got != "urgent" {
		t.Fatalf("Value = %v, want urgent", ev.Value)
	}
}

// Mixed-type enums (numbers + strings) preserve their stringified form so the
// wire response is a single comparable list.
func TestValidateConstraints_Enum_TypedError_MixedTypes(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"enum": []interface{}{1, 2, 3, "manual"},
	})
	err := ValidateConstraints(99, c)
	if err == nil {
		t.Fatal("expected enum violation")
	}
	var ev *EnumViolationError
	if !errors.As(err, &ev) {
		t.Fatalf("expected *EnumViolationError, got %T: %v", err, err)
	}
	if !reflect.DeepEqual(ev.AllowedValues, []string{"1", "2", "3", "manual"}) {
		t.Fatalf("AllowedValues = %v, want stringified mixed enum", ev.AllowedValues)
	}
}

// ---------------------------------------------------------------------------
// Combined constraints
// ---------------------------------------------------------------------------

func TestValidateConstraints_Combined_Pass(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"minLength": 1,
		"maxLength": 50,
		"regex":     `^[A-Z]`,
	})
	if err := ValidateConstraints("Hello", c); err != nil {
		t.Fatalf("expected pass, got: %v", err)
	}
}

func TestValidateConstraints_Combined_FailsFirst(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"minLength": 10,
		"regex":     `^[A-Z]`,
	})
	err := ValidateConstraints("Hi", c)
	if err == nil {
		t.Fatal("expected error for combined constraint violation")
	}
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestValidateConstraints_NilValue(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{
		"minLength": 1,
	})
	// nil values should pass constraint checks (nullability is handled elsewhere)
	if err := ValidateConstraints(nil, c); err != nil {
		t.Fatalf("nil value should pass constraints (nullability checked elsewhere), got: %v", err)
	}
}

func TestValidateConstraints_EmptyConstraints(t *testing.T) {
	c := constraintsJSON(map[string]interface{}{})
	if err := ValidateConstraints("anything", c); err != nil {
		t.Fatalf("empty constraints should pass, got: %v", err)
	}
}

func TestValidateConstraints_NullConstraints(t *testing.T) {
	if err := ValidateConstraints("anything", nil); err != nil {
		t.Fatalf("nil constraints should pass, got: %v", err)
	}
}

func TestValidateConstraints_InvalidJSON(t *testing.T) {
	err := ValidateConstraints("value", json.RawMessage(`{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON constraints")
	}
}
