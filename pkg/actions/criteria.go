package actions

import (
	"encoding/json"
	"fmt"
)

// SubmissionCriteria defines a condition that must be met before an action can execute.
type SubmissionCriteria struct {
	Type  string          `json:"type"`  // "always", "parameterMatch"
	Value json.RawMessage `json:"value,omitempty"`
}

// parameterMatchValue is the config for the "parameterMatch" criteria type.
type parameterMatchValue struct {
	Parameter string      `json:"parameter"`
	Operator  string      `json:"operator"` // "eq", "neq", "gt", "lt", "gte", "lte"
	Value     interface{} `json:"value"`
}

// ActionContext carries contextual information available during criteria evaluation.
type ActionContext struct {
	Parameters map[string]interface{}
	UserID     string
}

// EvaluateCriteria evaluates submission criteria against an action context.
// Returns nil if all criteria are satisfied, or an error describing the first failure.
// An empty or nil criteria JSON is treated as "always" (always allowed).
func EvaluateCriteria(criteriaJSON json.RawMessage, ctx ActionContext) error {
	if len(criteriaJSON) == 0 || string(criteriaJSON) == "null" || string(criteriaJSON) == "[]" {
		return nil
	}

	// Support either a single criterion object or an array of criteria.
	if criteriaJSON[0] == '[' {
		var criteria []SubmissionCriteria
		if err := json.Unmarshal(criteriaJSON, &criteria); err != nil {
			return fmt.Errorf("parse submission criteria: %w", err)
		}
		for _, c := range criteria {
			if err := evaluateSingleCriteria(c, ctx); err != nil {
				return err
			}
		}
		return nil
	}

	var c SubmissionCriteria
	if err := json.Unmarshal(criteriaJSON, &c); err != nil {
		return fmt.Errorf("parse submission criteria: %w", err)
	}
	return evaluateSingleCriteria(c, ctx)
}

func evaluateSingleCriteria(c SubmissionCriteria, ctx ActionContext) error {
	switch c.Type {
	case "always", "":
		return nil

	case "parameterMatch":
		var cfg parameterMatchValue
		if err := json.Unmarshal(c.Value, &cfg); err != nil {
			return fmt.Errorf("criteria parameterMatch: invalid config: %w", err)
		}
		paramVal, exists := ctx.Parameters[cfg.Parameter]
		if !exists {
			return fmt.Errorf("submission criteria not met: parameter %q not present", cfg.Parameter)
		}
		if err := compareValues(cfg.Parameter, paramVal, cfg.Operator, cfg.Value); err != nil {
			return fmt.Errorf("submission criteria not met: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("unknown submission criteria type: %q", c.Type)
	}
}

// compareValues performs an operator comparison between actual and expected values.
func compareValues(param string, actual interface{}, operator string, expected interface{}) error {
	switch operator {
	case "eq", "":
		if !jsonEqual(actual, expected) {
			return fmt.Errorf("parameter %q must equal %v (got %v)", param, expected, actual)
		}
	case "neq":
		if jsonEqual(actual, expected) {
			return fmt.Errorf("parameter %q must not equal %v", param, expected)
		}
	case "gt", "lt", "gte", "lte":
		a, aOk := toFloat64(actual)
		e, eOk := toFloat64(expected)
		if !aOk || !eOk {
			return fmt.Errorf("parameter %q: operator %q requires numeric values", param, operator)
		}
		switch operator {
		case "gt":
			if !(a > e) {
				return fmt.Errorf("parameter %q must be > %v (got %v)", param, expected, actual)
			}
		case "lt":
			if !(a < e) {
				return fmt.Errorf("parameter %q must be < %v (got %v)", param, expected, actual)
			}
		case "gte":
			if !(a >= e) {
				return fmt.Errorf("parameter %q must be >= %v (got %v)", param, expected, actual)
			}
		case "lte":
			if !(a <= e) {
				return fmt.Errorf("parameter %q must be <= %v (got %v)", param, expected, actual)
			}
		}
	default:
		return fmt.Errorf("unknown operator %q", operator)
	}
	return nil
}

func jsonEqual(a, b interface{}) bool {
	aj, _ := json.Marshal(a)
	bj, _ := json.Marshal(b)
	return string(aj) == string(bj)
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}
