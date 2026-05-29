package actions

import (
	"encoding/json"
	"fmt"
	"strings"
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
//   - "group" — composite AND/OR/NOT over nested criteria
//     (PRD-V2 Gap-A3 round 133; Foundry submissionCriteriaConjunction
//     parity, closes the "either A or B" / "C must NOT hold" gap
//     without a full expression DSL)
//
// Multiple criteria in a top-level array are AND-ed (all must pass).
// A full mini-expression DSL (CEL-lite / Goja) is deferred to a
// future round.
type SubmissionCriteria struct {
	Type  string          `json:"type"`
	Value json.RawMessage `json:"value,omitempty"`
}

// parameterMatchValue is the config for the "parameterMatch" criteria type.
type parameterMatchValue struct {
	Parameter string      `json:"parameter"`
	Operator  string      `json:"operator"` // "eq", "neq", "gt", "lt", "gte", "lte"
	Value     interface{} `json:"value"`
}

// groupValue is the config for the "group" composite criterion
// (Gap-A3 round 133). Operator is one of "and" / "or" / "not"
// (case-insensitive). Children may themselves be groups, enabling
// arbitrary nesting.
//
// Semantics:
//   - "and" — every child must pass; first failure returned
//     (matches the existing top-level array AND).
//     Zero children is vacuously true.
//   - "or"  — first passing child short-circuits; if none pass,
//     returns an aggregated "submission criteria not met"
//     error that mentions every child's failure so action
//     authors can debug all branches at once. Zero children
//     is vacuously false.
//   - "not" — exactly one child required; passes iff the child
//     FAILS. Empty or >1 children rejected as config error
//     so authoring mistakes surface loudly.
type groupValue struct {
	Operator string               `json:"operator"`
	Criteria []SubmissionCriteria `json:"criteria"`
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

	case "group":
		var cfg groupValue
		if err := json.Unmarshal(c.Value, &cfg); err != nil {
			return fmt.Errorf("criteria group: invalid config: %w", err)
		}
		return evaluateGroupCriteria(cfg, ctx)

	default:
		return fmt.Errorf("unknown submission criteria type: %q", c.Type)
	}
}

// ValidateCriteriaSchema walks a SubmissionCriteria JSON payload
// and verifies that its STRUCTURE is well-formed — without
// evaluating against any parameters. Intended for the admin
// CreateActionType / UpdateActionType handlers so authoring
// mistakes surface as 422 at save time instead of as
// "submission criteria not met: unknown type X" hours later when
// the first apply lands.
//
// Empty / null / `[]` criteria are valid (matches
// EvaluateCriteria's short-circuit). Groups recurse so deeply
// nested authoring mistakes surface with the inner-most error.
// PRD-V2 Gap-A3 round 135.
func ValidateCriteriaSchema(criteriaJSON json.RawMessage) error {
	if len(criteriaJSON) == 0 || string(criteriaJSON) == "null" || string(criteriaJSON) == "[]" {
		return nil
	}

	if criteriaJSON[0] == '[' {
		var arr []SubmissionCriteria
		if err := json.Unmarshal(criteriaJSON, &arr); err != nil {
			return fmt.Errorf("submission criteria: invalid JSON: %w", err)
		}
		for i, c := range arr {
			if err := validateSingleCriteriaSchema(c); err != nil {
				return fmt.Errorf("submission criteria[%d]: %w", i, err)
			}
		}
		return nil
	}

	var single SubmissionCriteria
	if err := json.Unmarshal(criteriaJSON, &single); err != nil {
		return fmt.Errorf("submission criteria: invalid JSON: %w", err)
	}
	return validateSingleCriteriaSchema(single)
}

//nolint:gocyclo // refactoring out of scope for this PR
func validateSingleCriteriaSchema(c SubmissionCriteria) error {
	switch c.Type {
	case "always", "":
		return nil

	case "parameterMatch":
		var cfg parameterMatchValue
		if err := json.Unmarshal(c.Value, &cfg); err != nil {
			return fmt.Errorf("parameterMatch: invalid config: %w", err)
		}
		if cfg.Parameter == "" {
			return fmt.Errorf("parameterMatch: parameter is required")
		}
		if err := validateOperator(cfg.Operator); err != nil {
			return fmt.Errorf("parameterMatch: %w", err)
		}
		return nil

	case "parameterCompare":
		var cfg parameterCompareValue
		if err := json.Unmarshal(c.Value, &cfg); err != nil {
			return fmt.Errorf("parameterCompare: invalid config: %w", err)
		}
		if cfg.LeftParameter == "" {
			return fmt.Errorf("parameterCompare: leftParameter is required")
		}
		if cfg.RightParameter == "" {
			return fmt.Errorf("parameterCompare: rightParameter is required")
		}
		if err := validateOperator(cfg.Operator); err != nil {
			return fmt.Errorf("parameterCompare: %w", err)
		}
		return nil

	case "group":
		var cfg groupValue
		if err := json.Unmarshal(c.Value, &cfg); err != nil {
			return fmt.Errorf("group: invalid config: %w", err)
		}
		op := strings.ToLower(cfg.Operator)
		switch op {
		case "and", "or":
			// any number of children allowed (including zero)
		case "not":
			if len(cfg.Criteria) != 1 {
				return fmt.Errorf("group NOT requires exactly one child, got %d",
					len(cfg.Criteria))
			}
		default:
			return fmt.Errorf("group: unknown operator %q (want and|or|not)",
				cfg.Operator)
		}
		for i, child := range cfg.Criteria {
			if err := validateSingleCriteriaSchema(child); err != nil {
				return fmt.Errorf("group child[%d]: %w", i, err)
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown submission criteria type: %q", c.Type)
	}
}

func validateOperator(op string) error {
	switch op {
	case "eq", "neq", "gt", "lt", "gte", "lte", "":
		return nil
	default:
		return fmt.Errorf("unknown operator %q (want eq|neq|gt|lt|gte|lte)", op)
	}
}

func evaluateGroupCriteria(cfg groupValue, ctx ActionContext) error {
	op := strings.ToLower(cfg.Operator)
	switch op {
	case "and":
		for _, child := range cfg.Criteria {
			if err := evaluateSingleCriteria(child, ctx); err != nil {
				return err
			}
		}
		return nil

	case "or":
		if len(cfg.Criteria) == 0 {
			return fmt.Errorf("submission criteria not met: empty OR group has no satisfiable children")
		}
		var failures []string
		for _, child := range cfg.Criteria {
			err := evaluateSingleCriteria(child, ctx)
			if err == nil {
				return nil
			}
			failures = append(failures, err.Error())
		}
		return fmt.Errorf("submission criteria not met: no OR child passed: [%s]",
			strings.Join(failures, " | "))

	case "not":
		if len(cfg.Criteria) != 1 {
			return fmt.Errorf("criteria group: NOT requires exactly one child, got %d",
				len(cfg.Criteria))
		}
		if err := evaluateSingleCriteria(cfg.Criteria[0], ctx); err == nil {
			return fmt.Errorf("submission criteria not met: NOT child unexpectedly passed")
		}
		return nil

	default:
		return fmt.Errorf("criteria group: unknown operator %q (want and|or|not)",
			cfg.Operator)
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
