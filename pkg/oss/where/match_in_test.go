package where

import (
	"encoding/json"
	"testing"
)

// TestMatchClause_In covers the in-memory matcher for the Foundry "in"
// operator (PR #293 follow-up): the SSE/WS subscription paths evaluate
// where clauses via MatchClause instead of Bleve, so `in` must behave as
// "equals ANY value in the array" with the exact same per-element type
// semantics as eq (numbers compare across Go numeric types via
// coerceNumber, booleans stay booleans, strings compare exactly). An empty
// array never matches; malformed values conservatively never match.
func TestMatchClause_In(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value string
		row   map[string]interface{}
		want  bool
	}{
		{
			name:  "string element matches",
			field: "name",
			value: `["alice","bob"]`,
			row:   map[string]interface{}{"name": "alice"},
			want:  true,
		},
		{
			name:  "no element matches",
			field: "name",
			value: `["alice","bob"]`,
			row:   map[string]interface{}{"name": "carol"},
			want:  false,
		},
		{
			name:  "string comparison is exact like eq (no case folding)",
			field: "name",
			value: `["Alice"]`,
			row:   map[string]interface{}{"name": "alice"},
			want:  false,
		},
		{
			name:  "numeric element matches float64 row value",
			field: "age",
			value: `[25, 30]`,
			row:   map[string]interface{}{"age": float64(25)},
			want:  true,
		},
		{
			name:  "numeric element matches int row value via cross-type coercion",
			field: "age",
			value: `[25]`,
			row:   map[string]interface{}{"age": int(25)},
			want:  true,
		},
		{
			name:  "numeric element does not match a different number",
			field: "age",
			value: `[25]`,
			row:   map[string]interface{}{"age": float64(26)},
			want:  false,
		},
		{
			name:  "numeric element never matches a bool row value",
			field: "age",
			value: `[1]`,
			row:   map[string]interface{}{"age": true},
			want:  false,
		},
		{
			name:  "boolean element matches",
			field: "active",
			value: `[false]`,
			row:   map[string]interface{}{"active": false},
			want:  true,
		},
		{
			name:  "mixed-type array matches on any element",
			field: "name",
			value: `[25, "alice"]`,
			row:   map[string]interface{}{"name": "alice"},
			want:  true,
		},
		{
			name:  "empty array never matches",
			field: "name",
			value: `[]`,
			row:   map[string]interface{}{"name": "alice"},
			want:  false,
		},
		{
			name:  "missing field never matches",
			field: "name",
			value: `["alice"]`,
			row:   map[string]interface{}{"other": "alice"},
			want:  false,
		},
		{
			name:  "non-array value conservatively never matches",
			field: "name",
			value: `"alice"`,
			row:   map[string]interface{}{"name": "alice"},
			want:  false,
		},
		{
			name:  "null value conservatively never matches",
			field: "name",
			value: `null`,
			row:   map[string]interface{}{"name": "alice"},
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := WhereClause{Type: "in", Field: tc.field, Value: json.RawMessage(tc.value)}
			if got := MatchClause(&c, tc.row); got != tc.want {
				t.Errorf("MatchClause(in %s vs %v) = %v, want %v", tc.value, tc.row, got, tc.want)
			}
		})
	}
}

// TestMatchClause_In_InsideLogicalOperators locks the composition contract:
// subscription filters routinely wrap `in` inside and/or/not trees.
func TestMatchClause_In_InsideLogicalOperators(t *testing.T) {
	clauseJSON := `{
		"type": "and",
		"value": [
			{"type": "in", "field": "status", "value": ["SHIPPED", "DELIVERED"]},
			{"type": "gt", "field": "amount", "value": 100}
		]
	}`
	var clause WhereClause
	if err := json.Unmarshal([]byte(clauseJSON), &clause); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !MatchClause(&clause, map[string]interface{}{"status": "SHIPPED", "amount": 150.0}) {
		t.Error("SHIPPED/150 should match in+gt")
	}
	if MatchClause(&clause, map[string]interface{}{"status": "PENDING", "amount": 150.0}) {
		t.Error("PENDING/150 should not match: status not in candidates")
	}
	if MatchClause(&clause, map[string]interface{}{"status": "DELIVERED", "amount": 50.0}) {
		t.Error("DELIVERED/50 should not match: amount too low")
	}
}

// TestValidateMatchClauseSupported_In: `in` must now be accepted by the
// pre-flight validator so scenario aggregation / SSE callers stop rejecting
// user filters the matcher actually supports.
func TestValidateMatchClauseSupported_In(t *testing.T) {
	clause := WhereClause{Type: "in", Field: "status", Value: json.RawMessage(`["A","B"]`)}
	if err := ValidateMatchClauseSupported(&clause); err != nil {
		t.Errorf("ValidateMatchClauseSupported(in) = %v, want nil", err)
	}

	nested := WhereClause{Type: "and", Value: json.RawMessage(`[
		{"type": "in", "field": "status", "value": ["A"]}
	]`)}
	if err := ValidateMatchClauseSupported(&nested); err != nil {
		t.Errorf("ValidateMatchClauseSupported(and>in) = %v, want nil", err)
	}
}
