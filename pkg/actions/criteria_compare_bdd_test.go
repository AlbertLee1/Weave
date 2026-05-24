package actions

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBDD_SubmissionCriteria_ParameterCompare covers PRD-V2 Gap-A3
// round 40: a new "parameterCompare" criteria type lets action
// authors express cross-field constraints like
// `endTime > startTime` or `discountedPrice <= listPrice` without
// having to wait for a full expression DSL (CEL-lite / Goja).
//
// Acceptance criteria (Given → When → Then):
//
//   Given criteria [{type:"parameterCompare",
//                    value:{leftParameter:"endTime",
//                           operator:"gt",
//                           rightParameter:"startTime"}}]
//         and an ActionContext where endTime=2 and startTime=1
//   When  EvaluateCriteria runs
//   Then  it returns nil (constraint satisfied)
//
//   Given the same criteria but endTime=1 and startTime=2
//   When  EvaluateCriteria runs
//   Then  it returns a "submission criteria not met" error
//         mentioning the left parameter name
//
//   Given criteria where rightParameter is missing from the
//         action context
//   When  EvaluateCriteria runs
//   Then  it returns "parameter X not present" naming the missing one
//
//   Given an array combining parameterCompare AND parameterMatch
//         (AND semantics — both must pass)
//   When  EvaluateCriteria runs against a context that satisfies
//         both
//   Then  it returns nil
//
//   Given an unknown operator inside parameterCompare
//   When  EvaluateCriteria runs
//   Then  it returns an unknown-operator error from compareValues
//
//   Given parameterCompare with leftParameter omitted (empty string)
//   When  EvaluateCriteria runs
//   Then  it returns a config-validation error mentioning
//         "leftParameter is required"
func TestBDD_SubmissionCriteria_ParameterCompare(t *testing.T) {
	t.Run("end > start: constraint satisfied", func(t *testing.T) {
		criteria := mustCriteriaJSON(t, []SubmissionCriteria{{
			Type: "parameterCompare",
			Value: mustJSONRaw(t, parameterCompareValue{
				LeftParameter:  "endTime",
				Operator:       "gt",
				RightParameter: "startTime",
			}),
		}})
		err := EvaluateCriteria(criteria, ActionContext{
			Parameters: map[string]interface{}{"endTime": 2, "startTime": 1},
		})
		if err != nil {
			t.Errorf("expected nil, got: %v", err)
		}
	})

	t.Run("end > start: constraint violated → not-met error mentions left param", func(t *testing.T) {
		criteria := mustCriteriaJSON(t, []SubmissionCriteria{{
			Type: "parameterCompare",
			Value: mustJSONRaw(t, parameterCompareValue{
				LeftParameter:  "endTime",
				Operator:       "gt",
				RightParameter: "startTime",
			}),
		}})
		err := EvaluateCriteria(criteria, ActionContext{
			Parameters: map[string]interface{}{"endTime": 1, "startTime": 2},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "submission criteria not met") {
			t.Errorf("err = %q, want it to mention 'submission criteria not met'", err.Error())
		}
		if !strings.Contains(err.Error(), "endTime") {
			t.Errorf("err = %q, want it to mention the left parameter name 'endTime'", err.Error())
		}
	})

	t.Run("missing rightParameter → 'parameter X not present'", func(t *testing.T) {
		criteria := mustCriteriaJSON(t, []SubmissionCriteria{{
			Type: "parameterCompare",
			Value: mustJSONRaw(t, parameterCompareValue{
				LeftParameter:  "endTime",
				Operator:       "gt",
				RightParameter: "startTime",
			}),
		}})
		err := EvaluateCriteria(criteria, ActionContext{
			// startTime missing from params
			Parameters: map[string]interface{}{"endTime": 2},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "startTime") {
			t.Errorf("err = %q, want it to name the missing parameter 'startTime'", err.Error())
		}
		if !strings.Contains(err.Error(), "not present") {
			t.Errorf("err = %q, want 'not present'", err.Error())
		}
	})

	t.Run("AND with parameterMatch: combined pass", func(t *testing.T) {
		criteria := mustCriteriaJSON(t, []SubmissionCriteria{
			{
				Type: "parameterMatch",
				Value: mustJSONRaw(t, parameterMatchValue{
					Parameter: "approved", Operator: "eq", Value: true,
				}),
			},
			{
				Type: "parameterCompare",
				Value: mustJSONRaw(t, parameterCompareValue{
					LeftParameter:  "endTime",
					Operator:       "gt",
					RightParameter: "startTime",
				}),
			},
		})
		err := EvaluateCriteria(criteria, ActionContext{
			Parameters: map[string]interface{}{
				"approved": true, "endTime": 2, "startTime": 1,
			},
		})
		if err != nil {
			t.Errorf("expected nil (both criteria pass), got: %v", err)
		}
	})

	t.Run("unknown operator → unknown-operator error", func(t *testing.T) {
		criteria := mustCriteriaJSON(t, []SubmissionCriteria{{
			Type: "parameterCompare",
			Value: mustJSONRaw(t, parameterCompareValue{
				LeftParameter:  "a",
				Operator:       "is-totally-bogus",
				RightParameter: "b",
			}),
		}})
		err := EvaluateCriteria(criteria, ActionContext{
			Parameters: map[string]interface{}{"a": 1, "b": 2},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "unknown operator") {
			t.Errorf("err = %q, want 'unknown operator'", err.Error())
		}
	})

	t.Run("missing leftParameter config field → required-field error", func(t *testing.T) {
		criteria := mustCriteriaJSON(t, []SubmissionCriteria{{
			Type: "parameterCompare",
			Value: mustJSONRaw(t, parameterCompareValue{
				// LeftParameter intentionally empty
				Operator:       "eq",
				RightParameter: "b",
			}),
		}})
		err := EvaluateCriteria(criteria, ActionContext{
			Parameters: map[string]interface{}{"b": 1},
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "leftParameter is required") {
			t.Errorf("err = %q, want 'leftParameter is required'", err.Error())
		}
	})

	t.Run("regression guard: parameterMatch still works unchanged", func(t *testing.T) {
		criteria := mustCriteriaJSON(t, []SubmissionCriteria{{
			Type: "parameterMatch",
			Value: mustJSONRaw(t, parameterMatchValue{
				Parameter: "n", Operator: "gt", Value: 0,
			}),
		}})
		err := EvaluateCriteria(criteria, ActionContext{
			Parameters: map[string]interface{}{"n": 5},
		})
		if err != nil {
			t.Errorf("parameterMatch backwards-compat: expected nil, got %v", err)
		}
	})
}

// mustCriteriaJSON marshals []SubmissionCriteria into the
// json.RawMessage shape EvaluateCriteria accepts.
func mustCriteriaJSON(t *testing.T, criteria []SubmissionCriteria) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(criteria)
	if err != nil {
		t.Fatalf("marshal criteria: %v", err)
	}
	return b
}

// mustJSONRaw is a tiny helper that lifts arbitrary values into
// json.RawMessage for the criteria Value field.
func mustJSONRaw(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
