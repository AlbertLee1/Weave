package cellsec

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/masking"
)

// US-488 — Engine.CompileForRow extracts the cell/row's markings from the
// row map's reserved `__markings` field (auth.MarkingsField) and exposes
// them to the CEL program under the top-level `marking` binding.
//
// Independent of `user.markings` (the caller's clearance), `marking` lets
// authors target rows carrying specific classification labels without
// needing to also stamp them as plain properties.

func TestBDD_US488_MarkingBinding_PIIRowMaskedRegardlessOfUserClearance(t *testing.T) {
	otRID := "ri.ontology.main.object-type.PIIRow"
	store := NewMemoryStore()
	// Mask fires when the row carries the PII marking AND the caller is not
	// in the auditors role — proves all three bindings (row absent, user,
	// marking) participate.
	mask := mkCELMask("m-us488", otRID, "row-1", "ssn", masking.MaskStrategyRedact,
		`"PII" in marking && !("auditors" in user.roles)`)
	if err := store.Create(context.Background(), mask); err != nil {
		t.Fatalf("seed: %v", err)
	}
	engine := New(store, nil)
	if err := engine.Reload(context.Background()); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	piiRow := map[string]any{
		"ssn":              "555-12-3456",
		auth.MarkingsField: []string{"PII", "INTERNAL"},
	}
	publicRow := map[string]any{
		"ssn":              "999-99-9999",
		auth.MarkingsField: []string{"PUBLIC"},
	}
	unmarkedRow := map[string]any{
		"ssn": "111-22-3333",
	}

	viewer := userWithMarkings("u:alice", []string{"viewer"}, "PII")
	auditor := userWithMarkings("u:auditor", []string{"auditors"}, "PII")

	// Cleared-but-not-auditor caller is still masked on PII-marked row.
	got, err := engine.CompileForRow(context.Background(), viewer, otRID, "row-1", piiRow)
	if err != nil {
		t.Fatalf("CompileForRow: %v", err)
	}
	if got["ssn"] != masking.MaskStrategyRedact {
		t.Fatalf("expected PII-marked row to mask SSN for viewer, got %v", got)
	}

	// Auditor caller bypasses the role gate even on PII-marked row.
	got, err = engine.CompileForRow(context.Background(), auditor, otRID, "row-1", piiRow)
	if err != nil {
		t.Fatalf("CompileForRow: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected auditor to bypass mask, got %v", got)
	}

	// Non-PII row is never masked.
	got, err = engine.CompileForRow(context.Background(), viewer, otRID, "row-1", publicRow)
	if err != nil {
		t.Fatalf("CompileForRow: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected PUBLIC-marked row to skip mask, got %v", got)
	}

	// Row with no markings field defaults to empty list, mask predicate is
	// false → no transforms.
	got, err = engine.CompileForRow(context.Background(), viewer, otRID, "row-1", unmarkedRow)
	if err != nil {
		t.Fatalf("CompileForRow: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected unmarked row to skip mask, got %v", got)
	}
}

func TestUS488_MarkingExtraction_TolerantOfRawShapes(t *testing.T) {
	// __markings can arrive as []string, []any, or a single string depending
	// on how upstream writers serialised the field; the engine should
	// normalise all three before handing them to CEL.
	cases := []struct {
		name string
		raw  any
		want bool // does `"PII" in marking` fire?
	}{
		{"[]string with PII", []string{"PII"}, true},
		{"[]any with PII string", []any{"PII", "INTERNAL"}, true},
		{"single string PII", "PII", true},
		{"empty []string", []string{}, false},
		{"nil", nil, false},
		{"unrelated type", 42, false},
	}

	otRID := "ot"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := NewMemoryStore()
			mask := mkCELMask("m-"+tc.name, otRID, "k", "p", masking.MaskStrategyRedact, `"PII" in marking`)
			if err := store.Create(context.Background(), mask); err != nil {
				t.Fatalf("seed: %v", err)
			}
			engine := New(store, nil)
			if err := engine.Reload(context.Background()); err != nil {
				t.Fatalf("Reload: %v", err)
			}
			row := map[string]any{"p": "x"}
			if tc.raw != nil {
				row[auth.MarkingsField] = tc.raw
			}
			viewer := userWithMarkings("u:alice", []string{"viewer"})
			got, err := engine.CompileForRow(context.Background(), viewer, otRID, "k", row)
			if err != nil {
				t.Fatalf("CompileForRow: %v", err)
			}
			masked := got["p"] == masking.MaskStrategyRedact
			if masked != tc.want {
				t.Fatalf("expected masked=%v for raw=%v, got transforms=%v", tc.want, tc.raw, got)
			}
		})
	}
}
