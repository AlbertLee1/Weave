package actions

import (
	"encoding/json"
	"fmt"
)

// SubmissionCriteria defines a condition that must be met before an action can execute.
//
// Supported types:
//   - "always" / "" — always passes
//   - "parameterMatch" — compare one parameter against a literal
//     value: parameter <op> value
//   - "parameterCompare" — compare two parameters against each
//     other: leftParameter <op> rightParameter (PRD-V2 Gap-A3
//     round 40; closes the "参数 A > 参数 B" example without a
//     full DSL)
//
// Multiple criteria in an array are AND-ed (all must pass). A
// full mini-expression DSL (CEL-lite / Goja) is deferred to a
// future round.
type SubmissionCriteria struct {
	Type  string          `json:"type"`  // "always", "parameterMatch", "parameterCompare"
	Value json.RawMessage `json:"value,omitempty"`
}

// parameterMatchValue is the config for the "parameterMatch" criteria type.
type parameterMatchValue struct {
	Parameter string      `json:"parameter"`
	Operator  string      `json:"operator"` // "eq", "neq", "gt", "lt", "gte", "lte"
	Value     interface{} `json:"value"`
}

// parameterCompareValue is the config for the "parameterCompare"
// criteria type (Gap-A3 round 40). Compares two parameters against
// each other so action authors can express constraints like
// `endTime > startTime` or `discountedPrice <= listPrice` without
// embedding a full expression DSL. Both LeftParameter and
// RightParameter must be present in the action's parameter map at
// evaluation time; missing parameters fail the criterion with a
// clear message.
type parameterCompareValue struct {
	LeftParameter  string `json:"leftParameter"`
	Operator       string `json:"operator"` // "eq", "neq", "gt", "lt", "gte", "lte"
	RightParameter string `json:"rightParameter"`
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

	case "parameterCompare":
		// PRD-V2 Gap-A3 round 40: cross-parameter comparison so
		// action authors can express e.g. endTime > startTime without
		// a full expression DSL.
		var cfg parameterCompareValue
		if err := json.Unmarshal(c.Value, &cfg); err != nil {
			return fmt.Errorf("criteria parameterCompare: invalid config: %w", err)
		}
		if cfg.LeftParameter == "" {
			return fmt.Errorf("criteria parameterCompare: leftParameter is required")
		}
		if cfg.RightParameter == "" {
			return fmt.Errorf("criteria parameterCompare: rightParameter is required")
		}
		leftVal, leftOK := ctx.Parameters[cfg.LeftParameter]
		if !leftOK {
			return fmt.Errorf("submission criteria not met: parameter %q not present", cfg.LeftParameter)
		}
		rightVal, rightOK := ctx.Parameters[cfg.RightParameter]
		if !rightOK {
			return fmt.Errorf("submission criteria not met: parameter %q not present", cfg.RightParameter)
		}
		// Reuse the existing compareValues helper — error messages
		// include the LEFT parameter name (since that's the one
		// "under test"); the right value gets rendered via the
		// helper's "got %v" suffix when applicable.
		if err := compareValues(cfg.LeftParameter, leftVal, cfg.Operator, rightVal); err != nil {
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
