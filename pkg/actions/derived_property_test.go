package actions

import (
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
)

// TestDerivedPropertyConstraints_ReadOnly covers US-004 part 3: any Action
// rule that tries to MODIFY or CREATE an Edit which writes to a property
// flagged derived must be rejected with DerivedPropertyReadOnly.
func TestDerivedPropertyConstraints_ReadOnly(t *testing.T) {
	derived := map[string]map[string]bool{
		"Customer": {"orderCount": true},
	}

	t.Run("modify derived property is rejected", func(t *testing.T) {
		edits := []funnel.Edit{{
			Type:       funnel.EditTypeModify,
			ObjectType: "Customer",
			PrimaryKey: "c1",
			Properties: map[string]interface{}{"orderCount": 42},
		}}
		err := ValidateEditsAgainstDerived(edits, derived)
		if err == nil {
			t.Fatal("expected error for modify on derived property, got nil")
		}
		if !errors.Is(err, ErrDerivedPropertyReadOnly) {
			t.Fatalf("expected ErrDerivedPropertyReadOnly, got %v", err)
		}
	})

	t.Run("create writing derived property is rejected", func(t *testing.T) {
		edits := []funnel.Edit{{
			Type:       funnel.EditTypeCreate,
			ObjectType: "Customer",
			PrimaryKey: "c2",
			Properties: map[string]interface{}{
				"name":       "Alice",
				"orderCount": 0,
			},
		}}
		err := ValidateEditsAgainstDerived(edits, derived)
		if err == nil {
			t.Fatal("expected error for create writing derived property, got nil")
		}
		if !errors.Is(err, ErrDerivedPropertyReadOnly) {
			t.Fatalf("expected ErrDerivedPropertyReadOnly, got %v", err)
		}
	})

	t.Run("modify non-derived property is allowed", func(t *testing.T) {
		edits := []funnel.Edit{{
			Type:       funnel.EditTypeModify,
			ObjectType: "Customer",
			PrimaryKey: "c1",
			Properties: map[string]interface{}{"name": "Bob"},
		}}
		if err := ValidateEditsAgainstDerived(edits, derived); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("delete edit is never blocked by derived rules", func(t *testing.T) {
		edits := []funnel.Edit{{
			Type:       funnel.EditTypeDelete,
			ObjectType: "Customer",
			PrimaryKey: "c1",
		}}
		if err := ValidateEditsAgainstDerived(edits, derived); err != nil {
			t.Fatalf("unexpected error on delete: %v", err)
		}
	})

	t.Run("unknown object type has no derived properties", func(t *testing.T) {
		edits := []funnel.Edit{{
			Type:       funnel.EditTypeModify,
			ObjectType: "Product",
			PrimaryKey: "p1",
			Properties: map[string]interface{}{"orderCount": 1},
		}}
		if err := ValidateEditsAgainstDerived(edits, derived); err != nil {
			t.Fatalf("unexpected error for unknown object type: %v", err)
		}
	})
}
