package actions

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBDD_ValidateCriteriaSchema covers PRD-V2 Gap-A3 round 135:
// static (no-context) validation of SubmissionCriteria JSON at
// ActionType save time. Before round 135 the admin handler would
// silently accept any criteria JSON and only blow up at the first
// apply attempt — admins got "submission criteria not met: unknown
// type X" surprises hours after saving.
//
// ValidateCriteriaSchema walks the criteria tree and checks STRUCTURE
// only — it does not evaluate against parameters (since no
// ActionContext exists at save time). The admin layer is expected
// to call this before persisting; cmd/server wires it in via the
// pkg/oms criteriaValidator hook.
//
// Acceptance criteria (Given → When → Then):
//
//	Given a valid criteria array of mixed parameterMatch +
//	      parameterCompare + group(and(...))
//	When  ValidateCriteriaSchema runs
//	Then  it returns nil
//
//	Given criteria with an unknown type
//	When  ValidateCriteriaSchema runs
//	Then  it returns an error naming the unknown type
//
//	Given a parameterMatch with an unknown operator
//	When  ValidateCriteriaSchema runs
//	Then  it returns an error naming the operator
//
//	Given a parameterMatch missing the parameter field
//	When  ValidateCriteriaSchema runs
//	Then  it returns an error mentioning "parameter"
//
//	Given a parameterCompare missing leftParameter
//	When  ValidateCriteriaSchema runs
//	Then  it returns an error mentioning "leftParameter"
//
//	Given a group with an unknown operator
//	When  ValidateCriteriaSchema runs
//	Then  it returns an error naming the operator
//
//	Given a NOT group whose children array is not exactly one
//	When  ValidateCriteriaSchema runs
//	Then  it returns an error mentioning NOT and the count
//
//	Given an empty JSON / null / empty array (no criteria)
//	When  ValidateCriteriaSchema runs
//	Then  it returns nil (matches EvaluateCriteria's empty
//	      short-circuit — vacuously valid)
//
//	Given malformed JSON (truncated object)
//	When  ValidateCriteriaSchema runs
//	Then  it returns a parse error mentioning "submission criteria"
//
//	Given a deeply nested group(AND(OR(NOT(parameterMatch)))) where
//	      the innermost parameterMatch has a bad operator
//	When  ValidateCriteriaSchema runs
//	Then  the error bubbles up from the inner validator (proves
//	      recursive validation, not just top-level)
//
// Tests written FIRST (RED) before adding ValidateCriteriaSchema.
func TestBDD_ValidateCriteriaSchema(t *testing.T) {
	t.Run("Valid mixed criteria returns nil", func(t *testing.T) {
		raw := json.RawMessage(`[
			{"type":"parameterMatch","value":{"parameter":"status","operator":"eq","value":"active"}},
			{"type":"parameterCompare","value":{"leftParameter":"endTime","operator":"gt","rightParameter":"startTime"}},
			{"type":"group","value":{"operator":"and","criteria":[
				{"type":"parameterMatch","value":{"parameter":"priority","operator":"gte","value":1}}
			]}}
		]`)
		if err := ValidateCriteriaSchema(raw); err != nil {
			t.Errorf("expected nil, got %v", err)
		}
	})

	t.Run("Unknown type rejected", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"weirdType"}`)
		err := ValidateCriteriaSchema(raw)
		if err == nil {
			t.Fatal("expected error for unknown type")
		}
		if !strings.Contains(err.Error(), "weirdType") {
			t.Errorf("expected error to name 'weirdType', got: %v", err)
		}
	})

	t.Run("parameterMatch unknown operator rejected", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"parameterMatch","value":{"parameter":"x","operator":"xor","value":"y"}}`)
		err := ValidateCriteriaSchema(raw)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "xor") {
			t.Errorf("expected error to name 'xor', got: %v", err)
		}
	})

	t.Run("parameterMatch missing parameter rejected", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"parameterMatch","value":{"operator":"eq","value":"y"}}`)
		err := ValidateCriteriaSchema(raw)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "parameter") {
			t.Errorf("expected error to mention 'parameter', got: %v", err)
		}
	})

	t.Run("parameterCompare missing leftParameter rejected", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"parameterCompare","value":{"operator":"gt","rightParameter":"a"}}`)
		err := ValidateCriteriaSchema(raw)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "leftParameter") {
			t.Errorf("expected error to mention 'leftParameter', got: %v", err)
		}
	})

	t.Run("parameterCompare missing rightParameter rejected", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"parameterCompare","value":{"operator":"gt","leftParameter":"a"}}`)
		err := ValidateCriteriaSchema(raw)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "rightParameter") {
			t.Errorf("expected error to mention 'rightParameter', got: %v", err)
		}
	})

	t.Run("group unknown operator rejected", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"group","value":{"operator":"xor","criteria":[]}}`)
		err := ValidateCriteriaSchema(raw)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "xor") {
			t.Errorf("expected error to name 'xor', got: %v", err)
		}
	})

	t.Run("NOT group with non-singleton children rejected", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"group","value":{"operator":"not","criteria":[
			{"type":"always"},
			{"type":"always"}
		]}}`)
		err := ValidateCriteriaSchema(raw)
		if err == nil {
			t.Fatal("expected error")
		}
		low := strings.ToLower(err.Error())
		if !strings.Contains(low, "not") {
			t.Errorf("expected error to mention NOT, got: %v", err)
		}
	})

	t.Run("Empty criteria treated as vacuously valid", func(t *testing.T) {
		cases := [][]byte{
			[]byte(``),
			[]byte(`null`),
			[]byte(`[]`),
		}
		for _, c := range cases {
			if err := ValidateCriteriaSchema(json.RawMessage(c)); err != nil {
				t.Errorf("empty/null criteria %q should be valid, got %v", string(c), err)
			}
		}
	})

	t.Run("Malformed JSON rejected", func(t *testing.T) {
		raw := json.RawMessage(`{"type":"parameterMatch",`) // truncated
		err := ValidateCriteriaSchema(raw)
		if err == nil {
			t.Fatal("expected parse error")
		}
		if !strings.Contains(err.Error(), "submission criteria") {
			t.Errorf("expected error to mention 'submission criteria', got: %v", err)
		}
	})

	t.Run("Deep nesting surfaces inner-most failure", func(t *testing.T) {
		// AND(OR(NOT(parameterMatch with bad op))) — innermost has
		// 'xor'. Validator must recurse through 3 group layers and
		// bubble the inner-most error.
		raw := json.RawMessage(`{"type":"group","value":{"operator":"and","criteria":[
			{"type":"group","value":{"operator":"or","criteria":[
				{"type":"group","value":{"operator":"not","criteria":[
					{"type":"parameterMatch","value":{"parameter":"p","operator":"xor","value":1}}
				]}}
			]}}
		]}}`)
		err := ValidateCriteriaSchema(raw)
		if err == nil {
			t.Fatal("expected nested-level error")
		}
		if !strings.Contains(err.Error(), "xor") {
			t.Errorf("expected error to bubble inner 'xor', got: %v", err)
		}
	})
}
