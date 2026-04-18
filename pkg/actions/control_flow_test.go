package actions

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/funnel"
)

// TestExecuteRules_If_TrueBranch verifies that an `if` rule with a satisfied
// condition runs its `then` body and skips `else`.
func TestExecuteRules_If_TrueBranch(t *testing.T) {
	rules := []Rule{
		{
			Type:      "if",
			Condition: &Condition{Parameter: "status", Operator: "eq", Value: "ACTIVE"},
			Then: []Rule{
				{
					Type:       "createObject",
					ObjectType: "Order",
					PropertyBindings: map[string]PropertyBinding{
						"status": {Type: "static", Value: "new"},
					},
				},
			},
			Else: []Rule{
				{Type: "deleteObject", ObjectType: "Order"},
			},
		},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{"status": "ACTIVE"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 || edits[0].Type != funnel.EditTypeCreate {
		t.Fatalf("expected single CREATE, got %+v", edits)
	}
}

// TestExecuteRules_If_FalseBranch verifies that a failing condition runs the
// `else` branch.
func TestExecuteRules_If_FalseBranch(t *testing.T) {
	rules := []Rule{
		{
			Type:      "if",
			Condition: &Condition{Parameter: "status", Operator: "eq", Value: "ACTIVE"},
			Then: []Rule{
				{Type: "createObject", ObjectType: "Order"},
			},
			Else: []Rule{
				{Type: "deleteObject", ObjectType: "Order"},
			},
		},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{
		"status":     "INACTIVE",
		"primaryKey": "o-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 || edits[0].Type != funnel.EditTypeDelete {
		t.Fatalf("expected single DELETE, got %+v", edits)
	}
}

// TestExecuteRules_If_NoElse skips silently when condition fails and no else.
func TestExecuteRules_If_NoElse(t *testing.T) {
	rules := []Rule{
		{
			Type:      "if",
			Condition: &Condition{Parameter: "flag", Operator: "truthy"},
			Then: []Rule{
				{Type: "createObject", ObjectType: "Order"},
			},
		},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{"flag": false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 0 {
		t.Fatalf("expected 0 edits, got %d", len(edits))
	}
}

// TestExecuteRules_If_LogicalOperators covers and/or/not condition trees.
func TestExecuteRules_If_LogicalOperators(t *testing.T) {
	rules := []Rule{
		{
			Type: "if",
			Condition: &Condition{
				Operator: "and",
				All: []Condition{
					{Parameter: "age", Operator: "gte", Value: float64(18)},
					{
						Operator: "or",
						Any: []Condition{
							{Parameter: "role", Operator: "eq", Value: "admin"},
							{Parameter: "role", Operator: "eq", Value: "editor"},
						},
					},
					{
						Operator: "not",
						Not:      &Condition{Parameter: "banned", Operator: "truthy"},
					},
				},
			},
			Then: []Rule{
				{Type: "createObject", ObjectType: "Privileged"},
			},
		},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{
		"age":    float64(21),
		"role":   "editor",
		"banned": false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
}

// TestExecuteRules_If_Operators exercises each supported comparison operator.
func TestExecuteRules_If_Operators(t *testing.T) {
	cases := []struct {
		name   string
		cond   Condition
		params map[string]interface{}
		want   bool
	}{
		{"eq true", Condition{Parameter: "x", Operator: "eq", Value: "a"}, map[string]interface{}{"x": "a"}, true},
		{"eq false", Condition{Parameter: "x", Operator: "eq", Value: "a"}, map[string]interface{}{"x": "b"}, false},
		{"ne true", Condition{Parameter: "x", Operator: "ne", Value: "a"}, map[string]interface{}{"x": "b"}, true},
		{"gt", Condition{Parameter: "n", Operator: "gt", Value: float64(10)}, map[string]interface{}{"n": float64(11)}, true},
		{"gte", Condition{Parameter: "n", Operator: "gte", Value: float64(10)}, map[string]interface{}{"n": float64(10)}, true},
		{"lt", Condition{Parameter: "n", Operator: "lt", Value: float64(10)}, map[string]interface{}{"n": float64(9)}, true},
		{"lte", Condition{Parameter: "n", Operator: "lte", Value: float64(10)}, map[string]interface{}{"n": float64(10)}, true},
		{"in true", Condition{Parameter: "x", Operator: "in", Value: []interface{}{"a", "b", "c"}}, map[string]interface{}{"x": "b"}, true},
		{"in false", Condition{Parameter: "x", Operator: "in", Value: []interface{}{"a", "b"}}, map[string]interface{}{"x": "z"}, false},
		{"notIn", Condition{Parameter: "x", Operator: "notIn", Value: []interface{}{"a"}}, map[string]interface{}{"x": "b"}, true},
		{"exists", Condition{Parameter: "x", Operator: "exists"}, map[string]interface{}{"x": nil}, true},
		{"notExists", Condition{Parameter: "y", Operator: "notExists"}, map[string]interface{}{"x": 1}, true},
		{"truthy string", Condition{Parameter: "x", Operator: "truthy"}, map[string]interface{}{"x": "hello"}, true},
		{"truthy empty string", Condition{Parameter: "x", Operator: "truthy"}, map[string]interface{}{"x": ""}, false},
		{"falsy nil", Condition{Parameter: "x", Operator: "falsy"}, map[string]interface{}{"x": nil}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rules := []Rule{{
				Type:      "if",
				Condition: &tc.cond,
				Then: []Rule{
					{Type: "createObject", ObjectType: "Marker"},
				},
			}}
			edits, err := ExecuteRules(rules, tc.params)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := len(edits) == 1
			if got != tc.want {
				t.Fatalf("condition evaluated to %v, want %v (edits=%d)", got, tc.want, len(edits))
			}
		})
	}
}

// TestExecuteRules_Foreach iterates over an array parameter and emits one edit
// per element.
func TestExecuteRules_Foreach(t *testing.T) {
	rules := []Rule{
		{
			Type:           "foreach",
			ItemsParameter: "users",
			ItemVariable:   "name",
			Rules: []Rule{
				{
					Type:       "createObject",
					ObjectType: "Employee",
					PropertyBindings: map[string]PropertyBinding{
						"name": {Type: "parameter", Value: "name"},
					},
				},
			},
		},
	}
	params := map[string]interface{}{
		"users": []interface{}{"Alice", "Bob", "Carol"},
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 3 {
		t.Fatalf("expected 3 edits, got %d", len(edits))
	}
	want := []string{"Alice", "Bob", "Carol"}
	for i, e := range edits {
		if e.Type != funnel.EditTypeCreate {
			t.Fatalf("edit %d: want CREATE, got %s", i, e.Type)
		}
		if e.Properties["name"] != want[i] {
			t.Fatalf("edit %d: want name=%s, got %v", i, want[i], e.Properties["name"])
		}
	}
}

// TestExecuteRules_Foreach_IndexVariable exposes the loop index to child rules.
func TestExecuteRules_Foreach_IndexVariable(t *testing.T) {
	rules := []Rule{
		{
			Type:           "foreach",
			ItemsParameter: "items",
			ItemVariable:   "item",
			IndexVariable:  "i",
			Rules: []Rule{
				{
					Type:       "createObject",
					ObjectType: "Row",
					PropertyBindings: map[string]PropertyBinding{
						"value": {Type: "parameter", Value: "item"},
						"index": {Type: "parameter", Value: "i"},
					},
				},
			},
		},
	}
	params := map[string]interface{}{
		"items": []interface{}{"a", "b"},
	}
	edits, err := ExecuteRules(rules, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 2 {
		t.Fatalf("expected 2 edits, got %d", len(edits))
	}
	if edits[0].Properties["index"] != 0 {
		t.Fatalf("edit 0 index want 0, got %v", edits[0].Properties["index"])
	}
	if edits[1].Properties["index"] != 1 {
		t.Fatalf("edit 1 index want 1, got %v", edits[1].Properties["index"])
	}
}

// TestExecuteRules_Foreach_EmptyItems is a successful no-op.
func TestExecuteRules_Foreach_EmptyItems(t *testing.T) {
	rules := []Rule{
		{
			Type:           "foreach",
			ItemsParameter: "items",
			ItemVariable:   "it",
			Rules: []Rule{
				{Type: "createObject", ObjectType: "X"},
			},
		},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{"items": []interface{}{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 0 {
		t.Fatalf("expected 0 edits, got %d", len(edits))
	}
}

// TestExecuteRules_Foreach_MissingItems reports a clear error.
func TestExecuteRules_Foreach_MissingItems(t *testing.T) {
	rules := []Rule{
		{
			Type:           "foreach",
			ItemsParameter: "missing",
			ItemVariable:   "it",
			Rules: []Rule{
				{Type: "createObject", ObjectType: "X"},
			},
		},
	}
	_, err := ExecuteRules(rules, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing-parameter error, got %v", err)
	}
}

// TestExecuteRules_Foreach_NonSlice rejects non-slice values.
func TestExecuteRules_Foreach_NonSlice(t *testing.T) {
	rules := []Rule{
		{
			Type:           "foreach",
			ItemsParameter: "x",
			ItemVariable:   "it",
			Rules: []Rule{
				{Type: "createObject", ObjectType: "X"},
			},
		},
	}
	_, err := ExecuteRules(rules, map[string]interface{}{"x": "not-a-slice"})
	if err == nil || !strings.Contains(err.Error(), "slice") {
		t.Fatalf("expected slice-required error, got %v", err)
	}
}

// TestExecuteRules_Switch picks the matching case.
func TestExecuteRules_Switch(t *testing.T) {
	rules := []Rule{
		{
			Type: "switch",
			On:   "level",
			Cases: []SwitchCase{
				{When: "low", Rules: []Rule{{Type: "createObject", ObjectType: "LowOrder"}}},
				{When: "high", Rules: []Rule{{Type: "createObject", ObjectType: "HighOrder"}}},
			},
			Default: []Rule{{Type: "createObject", ObjectType: "UnknownOrder"}},
		},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{"level": "high"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 || edits[0].ObjectType != "HighOrder" {
		t.Fatalf("expected single HighOrder, got %+v", edits)
	}
}

// TestExecuteRules_Switch_DefaultBranch falls through to default when no case
// matches.
func TestExecuteRules_Switch_DefaultBranch(t *testing.T) {
	rules := []Rule{
		{
			Type: "switch",
			On:   "level",
			Cases: []SwitchCase{
				{When: "low", Rules: []Rule{{Type: "createObject", ObjectType: "LowOrder"}}},
			},
			Default: []Rule{{Type: "createObject", ObjectType: "DefaultOrder"}},
		},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{"level": "unknown"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 1 || edits[0].ObjectType != "DefaultOrder" {
		t.Fatalf("expected DefaultOrder, got %+v", edits)
	}
}

// TestExecuteRules_Switch_NoMatchNoDefault is a successful no-op.
func TestExecuteRules_Switch_NoMatchNoDefault(t *testing.T) {
	rules := []Rule{
		{
			Type: "switch",
			On:   "level",
			Cases: []SwitchCase{
				{When: "low", Rules: []Rule{{Type: "createObject", ObjectType: "LowOrder"}}},
			},
		},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{"level": "other"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 0 {
		t.Fatalf("expected 0 edits, got %d", len(edits))
	}
}

// TestExecuteRules_NestedControlFlow exercises if → foreach → switch nesting.
func TestExecuteRules_NestedControlFlow(t *testing.T) {
	rules := []Rule{
		{
			Type:      "if",
			Condition: &Condition{Parameter: "enabled", Operator: "truthy"},
			Then: []Rule{
				{
					Type:           "foreach",
					ItemsParameter: "orders",
					ItemVariable:   "status",
					Rules: []Rule{
						{
							Type: "switch",
							On:   "status",
							Cases: []SwitchCase{
								{When: "pending", Rules: []Rule{{Type: "createObject", ObjectType: "Pending"}}},
								{When: "paid", Rules: []Rule{{Type: "createObject", ObjectType: "Paid"}}},
							},
						},
					},
				},
			},
		},
	}
	edits, err := ExecuteRules(rules, map[string]interface{}{
		"enabled": true,
		"orders":  []interface{}{"pending", "paid", "pending"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(edits) != 3 {
		t.Fatalf("expected 3 edits, got %d", len(edits))
	}
	want := []string{"Pending", "Paid", "Pending"}
	for i, e := range edits {
		if e.ObjectType != want[i] {
			t.Fatalf("edit %d: want %s, got %s", i, want[i], e.ObjectType)
		}
	}
}

// TestExecuteRules_DepthLimitExceeded rejects nesting deeper than 5.
func TestExecuteRules_DepthLimitExceeded(t *testing.T) {
	// Build 6 nested if rules — the 6th level should trip the limit.
	inner := []Rule{{Type: "createObject", ObjectType: "X"}}
	for i := 0; i < 6; i++ {
		inner = []Rule{{
			Type:      "if",
			Condition: &Condition{Parameter: "f", Operator: "truthy"},
			Then:      inner,
		}}
	}
	_, err := ExecuteRules(inner, map[string]interface{}{"f": true})
	if err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatalf("expected nesting-depth error, got %v", err)
	}
}

// TestExecuteRules_DepthLimitExactlyAtMax allows the maximum nesting depth.
func TestExecuteRules_DepthLimitExactlyAtMax(t *testing.T) {
	inner := []Rule{{Type: "createObject", ObjectType: "Deep"}}
	// MaxRuleNestingDepth wraps should still succeed. The simple rule itself
	// doesn't count towards depth; every control-flow wrapper does.
	for i := 0; i < MaxRuleNestingDepth; i++ {
		inner = []Rule{{
			Type:      "if",
			Condition: &Condition{Parameter: "f", Operator: "truthy"},
			Then:      inner,
		}}
	}
	edits, err := ExecuteRules(inner, map[string]interface{}{"f": true})
	if err != nil {
		t.Fatalf("unexpected error at max depth: %v", err)
	}
	if len(edits) != 1 {
		t.Fatalf("expected 1 edit, got %d", len(edits))
	}
}

// TestExecuteRules_If_MissingCondition reports a structured error.
func TestExecuteRules_If_MissingCondition(t *testing.T) {
	rules := []Rule{{Type: "if", Then: []Rule{{Type: "createObject", ObjectType: "X"}}}}
	_, err := ExecuteRules(rules, map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "condition") {
		t.Fatalf("expected missing-condition error, got %v", err)
	}
}

// TestParseRules_ControlFlow round-trips the JSON shape so action authors can
// declare these rules alongside the existing set.
func TestParseRules_ControlFlow(t *testing.T) {
	raw := json.RawMessage(`[
		{
			"type": "if",
			"condition": {"parameter": "flag", "operator": "truthy"},
			"then": [
				{"type": "foreach", "itemsParameter": "xs", "itemVariable": "x", "rules": [
					{"type": "switch", "on": "x", "cases": [
						{"when": "a", "rules": [{"type": "createObject", "objectType": "A"}]}
					], "default": [{"type": "createObject", "objectType": "Other"}]}
				]}
			]
		}
	]`)
	rules, err := ParseRules(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rules) != 1 || rules[0].Type != "if" {
		t.Fatalf("unexpected rules: %+v", rules)
	}
	if rules[0].Condition == nil || rules[0].Condition.Operator != "truthy" {
		t.Fatalf("missing condition: %+v", rules[0].Condition)
	}
	if len(rules[0].Then) != 1 || rules[0].Then[0].Type != "foreach" {
		t.Fatalf("then branch malformed: %+v", rules[0].Then)
	}
	inner := rules[0].Then[0]
	if len(inner.Rules) != 1 || inner.Rules[0].Type != "switch" {
		t.Fatalf("foreach body malformed: %+v", inner.Rules)
	}
	sw := inner.Rules[0]
	if len(sw.Cases) != 1 || sw.Cases[0].When != "a" {
		t.Fatalf("switch cases malformed: %+v", sw.Cases)
	}
	if len(sw.Default) != 1 {
		t.Fatalf("switch default malformed: %+v", sw.Default)
	}
}
