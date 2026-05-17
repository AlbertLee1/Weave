package celmask

import (
	"strings"
	"testing"
)

// US-488 — Cell-level masking CEL gains a third top-level binding `marking`
// (list<string>) that exposes the cell/row's classification markings,
// independent of the caller's clearance (user.markings). Authors can then
// write predicates like `"PII" in marking` to mask cells carrying a marking
// label even when the caller happens to be cleared for it.
//
// The acceptance suite below pins:
//   - `marking` compiles as a top-level list<string> variable
//   - empty / nil markings default to an empty list (no eval error)
//   - composite predicates over (user, row, marking) all return correct bools
//   - negative case: writing `marking.foo` (treating it like a map) is a
//     compile-time type error so authoring mistakes surface at Validate time

func TestUS488_Compile_MarkingVariable_TopLevelListBinding(t *testing.T) {
	// Should compile against the env now that `marking` is a known variable.
	if _, err := Compile(`"PII" in marking`); err != nil {
		t.Fatalf("expected `\"PII\" in marking` to compile, got %v", err)
	}
	if _, err := Compile(`size(marking) > 0`); err != nil {
		t.Fatalf("expected `size(marking) > 0` to compile, got %v", err)
	}
}

func TestUS488_Eval_MarkingBinding_FiresOnLabelMatch(t *testing.T) {
	prog, err := Compile(`"PII" in marking`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		name     string
		marking  []string
		expected bool
	}{
		{"cell carries PII marking", []string{"PII"}, true},
		{"cell carries unrelated marking", []string{"PUBLIC"}, false},
		{"cell carries PII among others", []string{"INTERNAL", "PII", "CONFIDENTIAL"}, true},
		{"cell has no markings", nil, false},
		{"cell has empty marking list", []string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := prog.EvalWithMarking(UserView{}, nil, tc.marking)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if got != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestUS488_Eval_ThreeWayBinding_UserRowMarking(t *testing.T) {
	// PRD-flavoured: mask the cell when the row is non-US OR the cell is
	// PII-labelled AND the caller is not in the finance role.
	prog, err := Compile(`row.country != "US" || ("PII" in marking && !("finance" in user.roles))`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	cases := []struct {
		name     string
		user     UserView
		row      map[string]any
		marking  []string
		expected bool
	}{
		{
			"US row, finance caller, PII cell → allowed",
			UserView{Roles: []string{"finance"}},
			map[string]any{"country": "US"},
			[]string{"PII"},
			false,
		},
		{
			"US row, viewer caller, PII cell → masked",
			UserView{Roles: []string{"viewer"}},
			map[string]any{"country": "US"},
			[]string{"PII"},
			true,
		},
		{
			"CN row → masked regardless of caller",
			UserView{Roles: []string{"finance"}},
			map[string]any{"country": "CN"},
			[]string{"PUBLIC"},
			true,
		},
		{
			"US row, no PII marking → allowed",
			UserView{Roles: []string{"viewer"}},
			map[string]any{"country": "US"},
			[]string{"PUBLIC"},
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := prog.EvalWithMarking(tc.user, tc.row, tc.marking)
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if got != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestUS488_Eval_LegacyEvalDefaultsMarkingToEmpty(t *testing.T) {
	// Legacy Eval (no markings arg) still works; expressions referencing
	// `marking` see an empty list and return the natural false.
	prog, err := Compile(`"PII" in marking`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := prog.Eval(UserView{}, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got {
		t.Fatalf("legacy Eval should default marking to empty list (false), got %v", got)
	}
}

func TestUS488_Compile_NegativeMarkingMapAccess_FailsTypeCheck(t *testing.T) {
	// `marking` is a list, not a map: accessing it via `.foo` must fail at
	// compile time so authoring mistakes never reach runtime.
	if _, err := Compile(`marking.foo == "x"`); err == nil {
		t.Fatalf("expected compile error treating list `marking` as a map")
	} else if !strings.Contains(strings.ToLower(err.Error()), "marking") &&
		!strings.Contains(strings.ToLower(err.Error()), "list") {
		t.Logf("error did not reference marking/list verbatim (acceptable): %v", err)
	}
}
