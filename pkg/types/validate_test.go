package types

import "testing"

func TestValidate_String_Valid(t *testing.T) {
	err := Validate("hello", DataType{Type: String}, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_String_Invalid(t *testing.T) {
	err := Validate(123, DataType{Type: String}, false)
	if err == nil {
		t.Fatal("expected error for int value on string type")
	}
}

func TestValidate_Integer_Valid(t *testing.T) {
	err := Validate(42, DataType{Type: Integer}, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_Integer_Invalid(t *testing.T) {
	err := Validate("abc", DataType{Type: Integer}, false)
	if err == nil {
		t.Fatal("expected error for string value on integer type")
	}
}

func TestValidate_Long_Valid(t *testing.T) {
	err := Validate(int64(9223372036854775807), DataType{Type: Long}, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_Long_AcceptsString(t *testing.T) {
	err := Validate("9223372036854775807", DataType{Type: Long}, false)
	if err != nil {
		t.Fatalf("expected no error for string long value, got %v", err)
	}
}

func TestValidate_Boolean_Valid(t *testing.T) {
	err := Validate(true, DataType{Type: Boolean}, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_Boolean_Invalid(t *testing.T) {
	err := Validate("yes", DataType{Type: Boolean}, false)
	if err == nil {
		t.Fatal("expected error for string value on boolean type")
	}
}

func TestValidate_Date_Valid(t *testing.T) {
	err := Validate("2024-01-15", DataType{Type: Date}, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_Date_Invalid(t *testing.T) {
	err := Validate("01/15/2024", DataType{Type: Date}, false)
	if err == nil {
		t.Fatal("expected error for invalid date format")
	}
}

func TestValidate_Timestamp_Valid(t *testing.T) {
	err := Validate("2024-01-15T10:30:00Z", DataType{Type: Timestamp}, false)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidate_Timestamp_Invalid(t *testing.T) {
	err := Validate("not-a-timestamp", DataType{Type: Timestamp}, false)
	if err == nil {
		t.Fatal("expected error for invalid timestamp")
	}
}

func TestValidate_Nil_Nullable(t *testing.T) {
	err := Validate(nil, DataType{Type: String}, true)
	if err != nil {
		t.Fatalf("expected no error for nil with nullable=true, got %v", err)
	}
}

func TestValidate_Nil_NonNullable(t *testing.T) {
	err := Validate(nil, DataType{Type: String}, false)
	if err == nil {
		t.Fatal("expected error for nil with nullable=false")
	}
}
