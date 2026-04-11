package oms

import (
	"strings"
	"testing"
)

// TestDerivedPropertyConstraints_PrimaryKey covers US-004 part 1: a property
// flagged as Derived must never be usable as an ObjectType primary key. The
// validator should surface DerivedPropertyNotAllowedAsPrimaryKey via apierror.
func TestDerivedPropertyConstraints_PrimaryKey(t *testing.T) {
	props := []Property{
		{APIName: "customerId", BaseType: "string"},
		{APIName: "orderCount", BaseType: "integer", Derived: true},
	}

	t.Run("non-derived primary key is accepted", func(t *testing.T) {
		if err := ValidateObjectTypePrimaryKey("customerId", props); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("derived property rejected as primary key", func(t *testing.T) {
		err := ValidateObjectTypePrimaryKey("orderCount", props)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.ErrorName != "DerivedPropertyNotAllowedAsPrimaryKey" {
			t.Fatalf("wrong error name: %q", err.ErrorName)
		}
		if err.ErrorCode != "INVALID_ARGUMENT" {
			t.Fatalf("expected INVALID_ARGUMENT, got %q", err.ErrorCode)
		}
		if got := err.Parameters["apiName"]; got != "orderCount" {
			t.Fatalf("expected apiName=orderCount, got %q", got)
		}
	})

	t.Run("primary key referencing missing property is rejected", func(t *testing.T) {
		err := ValidateObjectTypePrimaryKey("ghost", props)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(strings.ToLower(err.ErrorName), "primarykey") {
			t.Fatalf("expected error about primary key, got %q", err.ErrorName)
		}
	})

	t.Run("empty primary key is rejected", func(t *testing.T) {
		if err := ValidateObjectTypePrimaryKey("", props); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
