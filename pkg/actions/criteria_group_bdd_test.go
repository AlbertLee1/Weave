package actions

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBDD_SubmissionCriteria_Group covers PRD-V2 Gap-A3 round 133:
// a new "group" composite criterion that lets action authors
// express AND / OR / NOT combinations without a full expression DSL.
// Before round 133 the top-level array AND-ed all criteria; there
// was NO way to express "either A or B must hold" or "C must NOT
// hold" without writing the rule in JavaScript.
//
// Wire shape (Foundry submissionCriteriaConjunction parity):
//
//   {
//     "type": "group",
//     "value": {
//       "operator": "and" | "or" | "not",
//       "criteria": [<SubmissionCriteria>, ...]
//     }
//   }
//
// - "and": every child must pass (same as the top-level array)
// - "or":  at least one child must pass; aggregated error otherwise
// - "not": the single child must FAIL; passes if child fails
//
// Acceptance criteria (Given → When → Then):
//
//   Given a group with operator=or and two children where the
//         first fails and the second passes
//   When  EvaluateCriteria runs
//   Then  it returns nil (OR short-circuits on first pass)
//
//   Given a group with operator=or where both children fail
//   When  EvaluateCriteria runs
//   Then  it returns a "submission criteria not met" error whose
//         message mentions BOTH child failures (aggregated)
//
//   Given a group with operator=and where all children pass
//   When  EvaluateCriteria runs
//   Then  it returns nil
//
//   Given a group with operator=and where one child fails
//   When  EvaluateCriteria runs
//   Then  it returns the first failing child's error
//
//   Given a group with operator=not whose single child PASSES
//   When  EvaluateCriteria runs
//   Then  it returns a "submission criteria not met" error
//         (NOT negates the inner result)
//
//   Given a group with operator=not whose single child FAILS
//   When  EvaluateCriteria runs
//   Then  it returns nil
//
//   Given a group nested two deep — outer AND[ inner OR[a,b], c ]
//   When  EvaluateCriteria runs against a context where 'a' fails,
//         'b' passes, and 'c' passes
//   Then  it returns nil (OR short-circuits on b, outer AND passes)
//
//   Given a group with an unknown operator
//   When  EvaluateCriteria runs
//   Then  it returns an error mentioning the unknown operator
//
//   Given a group with operator=not but the children array is empty
//   When  EvaluateCriteria runs
//   Then  it returns a config-error mentioning that NOT requires
//         exactly one child (defensive — Foundry rejects this at
//         metadata-validation time)
//
// Tests are written FIRST (RED) before adding the "group" branch
// to evaluateSingleCriteria — confirming the existing implementation
// rejects unknown types with the current error message.
func TestBDD_SubmissionCriteria_Group(t *testing.T) {
	t.Run("OR with one passing child returns nil", func(t *testing.T) {
		// Child A: status must equal "active" — will FAIL (status=draft)
		// Child B: priority must be > 0 — will PASS (priority=5)
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "or",
				"criteria": [
					{"type":"parameterMatch","value":{"parameter":"status","operator":"eq","value":"active"}},
					{"type":"parameterMatch","value":{"parameter":"priority","operator":"gt","value":0}}
				]
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{
			"status":   "draft",
			"priority": 5,
		}}
		if err := EvaluateCriteria(criteria, ctx); err != nil {
			t.Errorf("OR with one passing child should return nil, got %v", err)
		}
	})

	t.Run("OR with all children failing returns aggregated error", func(t *testing.T) {
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "or",
				"criteria": [
					{"type":"parameterMatch","value":{"parameter":"status","operator":"eq","value":"active"}},
					{"type":"parameterMatch","value":{"parameter":"priority","operator":"gt","value":10}}
				]
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{
			"status":   "draft",
			"priority": 5,
		}}
		err := EvaluateCriteria(criteria, ctx)
		if err == nil {
			t.Fatal("OR with all failing should return an error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "submission criteria not met") {
			t.Errorf("expected 'submission criteria not met' in error, got: %s", msg)
		}
		// Both child failures aggregated — message should mention
		// both parameters (status AND priority).
		if !strings.Contains(msg, "status") || !strings.Contains(msg, "priority") {
			t.Errorf("expected aggregated error to mention BOTH 'status' and 'priority', got: %s", msg)
		}
	})

	t.Run("AND with all children passing returns nil", func(t *testing.T) {
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "and",
				"criteria": [
					{"type":"parameterMatch","value":{"parameter":"status","operator":"eq","value":"active"}},
					{"type":"parameterMatch","value":{"parameter":"priority","operator":"gt","value":0}}
				]
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{
			"status":   "active",
			"priority": 5,
		}}
		if err := EvaluateCriteria(criteria, ctx); err != nil {
			t.Errorf("AND with all passing should return nil, got %v", err)
		}
	})

	t.Run("AND with one failing child returns the failure", func(t *testing.T) {
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "and",
				"criteria": [
					{"type":"parameterMatch","value":{"parameter":"status","operator":"eq","value":"active"}},
					{"type":"parameterMatch","value":{"parameter":"priority","operator":"gt","value":100}}
				]
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{
			"status":   "active",
			"priority": 5,
		}}
		err := EvaluateCriteria(criteria, ctx)
		if err == nil {
			t.Fatal("AND with one failing should return an error")
		}
		if !strings.Contains(err.Error(), "priority") {
			t.Errorf("expected error to mention failing parameter 'priority', got: %v", err)
		}
	})

	t.Run("NOT of passing child returns error", func(t *testing.T) {
		// Inner child passes (status==active), NOT should fail.
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "not",
				"criteria": [
					{"type":"parameterMatch","value":{"parameter":"status","operator":"eq","value":"active"}}
				]
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{"status": "active"}}
		err := EvaluateCriteria(criteria, ctx)
		if err == nil {
			t.Fatal("NOT of passing child should return an error")
		}
		if !strings.Contains(err.Error(), "submission criteria not met") {
			t.Errorf("expected 'submission criteria not met', got: %v", err)
		}
	})

	t.Run("NOT of failing child returns nil", func(t *testing.T) {
		// Inner child fails (status != active), NOT should pass.
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "not",
				"criteria": [
					{"type":"parameterMatch","value":{"parameter":"status","operator":"eq","value":"active"}}
				]
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{"status": "draft"}}
		if err := EvaluateCriteria(criteria, ctx); err != nil {
			t.Errorf("NOT of failing child should return nil, got %v", err)
		}
	})

	t.Run("Nested groups (AND[OR[a,b], c]) evaluate correctly", func(t *testing.T) {
		// a fails, b passes → inner OR passes; c passes → outer AND
		// passes. Proves nested evaluation, not just one level.
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "and",
				"criteria": [
					{
						"type": "group",
						"value": {
							"operator": "or",
							"criteria": [
								{"type":"parameterMatch","value":{"parameter":"a","operator":"eq","value":"x"}},
								{"type":"parameterMatch","value":{"parameter":"b","operator":"eq","value":"y"}}
							]
						}
					},
					{"type":"parameterMatch","value":{"parameter":"c","operator":"eq","value":"z"}}
				]
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{
			"a": "wrong",
			"b": "y",
			"c": "z",
		}}
		if err := EvaluateCriteria(criteria, ctx); err != nil {
			t.Errorf("nested group should pass, got: %v", err)
		}
	})

	t.Run("Unknown operator returns config error", func(t *testing.T) {
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "xor",
				"criteria": [
					{"type":"parameterMatch","value":{"parameter":"status","operator":"eq","value":"active"}}
				]
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{"status": "active"}}
		err := EvaluateCriteria(criteria, ctx)
		if err == nil {
			t.Fatal("unknown operator should error")
		}
		if !strings.Contains(err.Error(), "xor") {
			t.Errorf("expected error to name unknown operator 'xor', got: %v", err)
		}
	})

	t.Run("NOT with non-singleton children array errors", func(t *testing.T) {
		// NOT semantics only makes sense with exactly one child; reject
		// the metadata at evaluation time rather than silently picking
		// one. Foundry validates this at action-type save time; we
		// surface it at evaluation as a clear config error.
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "not",
				"criteria": []
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{}}
		err := EvaluateCriteria(criteria, ctx)
		if err == nil {
			t.Fatal("NOT with empty children should error")
		}
		if !strings.Contains(err.Error(), "not") && !strings.Contains(err.Error(), "NOT") {
			t.Errorf("expected error to mention NOT operator, got: %v", err)
		}
	})

	t.Run("Group with empty children + AND passes (vacuous truth)", func(t *testing.T) {
		// AND over zero children is vacuously true — matches the
		// existing top-level `[]` empty-array short-circuit. Locks
		// in symmetry so users can compose groups programmatically
		// without special-casing the empty case.
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "and",
				"criteria": []
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{}}
		if err := EvaluateCriteria(criteria, ctx); err != nil {
			t.Errorf("empty AND should be vacuously true, got: %v", err)
		}
	})

	t.Run("Group with empty children + OR fails (vacuous falsity)", func(t *testing.T) {
		// OR over zero children is vacuously false — no child can
		// possibly pass. Reject as a clear "no criteria satisfied".
		criteria := json.RawMessage(`{
			"type": "group",
			"value": {
				"operator": "or",
				"criteria": []
			}
		}`)
		ctx := ActionContext{Parameters: map[string]interface{}{}}
		err := EvaluateCriteria(criteria, ctx)
		if err == nil {
			t.Fatal("empty OR should fail (no child can pass)")
		}
		if !strings.Contains(err.Error(), "submission criteria not met") {
			t.Errorf("expected 'submission criteria not met', got: %v", err)
		}
	})
}
