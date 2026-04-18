package actions

import (
	"encoding/json"
	"fmt"
)

// toFloat64 lives in criteria.go. Conditions reuse it for numeric comparisons
// so enum / numeric semantics stay in lockstep.

// evaluateCondition reports whether the condition holds for the given
// parameters. Leaf operators read params[c.Parameter]; logical operators
// recurse into All / Any / Not.
func evaluateCondition(c *Condition, params map[string]interface{}) (bool, error) {
	if c == nil {
		return false, fmt.Errorf("condition is nil")
	}
	switch c.Operator {
	case "and":
		for i := range c.All {
			ok, err := evaluateCondition(&c.All[i], params)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	case "or":
		for i := range c.Any {
			ok, err := evaluateCondition(&c.Any[i], params)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case "not":
		if c.Not == nil {
			return false, fmt.Errorf("not: inner condition is required")
		}
		ok, err := evaluateCondition(c.Not, params)
		if err != nil {
			return false, err
		}
		return !ok, nil
	}

	if c.Parameter == "" {
		return false, fmt.Errorf("condition operator %q requires a parameter", c.Operator)
	}
	v, present := params[c.Parameter]

	switch c.Operator {
	case "exists":
		return present, nil
	case "notExists":
		return !present, nil
	case "truthy":
		return isTruthy(v), nil
	case "falsy":
		return !isTruthy(v), nil
	}

	if !present {
		return false, fmt.Errorf("parameter %q not found", c.Parameter)
	}

	switch c.Operator {
	case "eq":
		return valuesEqual(v, c.Value), nil
	case "ne":
		return !valuesEqual(v, c.Value), nil
	case "gt", "gte", "lt", "lte":
		return compareNumeric(v, c.Value, c.Operator)
	case "in":
		return containsValue(c.Value, v)
	case "notIn":
		ok, err := containsValue(c.Value, v)
		if err != nil {
			return false, err
		}
		return !ok, nil
	}
	return false, fmt.Errorf("unknown operator: %q", c.Operator)
}

// isTruthy follows JS-style truthiness: nil, false, "", 0, empty slice/map → false.
func isTruthy(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case float32:
		return x != 0
	case int:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case []interface{}:
		return len(x) > 0
	case map[string]interface{}:
		return len(x) > 0
	}
	return true
}

// valuesEqual compares two values JSON-decoded side: numeric values are
// normalised via toFloat64; other types fall back to reflect-agnostic equality.
func valuesEqual(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == b
	}
	if af, aok := toFloat64(a); aok {
		if bf, bok := toFloat64(b); bok {
			return af == bf
		}
	}
	switch av := a.(type) {
	case string:
		if bv, ok := b.(string); ok {
			return av == bv
		}
	case bool:
		if bv, ok := b.(bool); ok {
			return av == bv
		}
	}
	aj, err1 := json.Marshal(a)
	bj, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(aj) == string(bj)
}

func compareNumeric(a, b interface{}, op string) (bool, error) {
	af, aok := toFloat64(a)
	bf, bok := toFloat64(b)
	if !aok || !bok {
		return false, fmt.Errorf("operator %q requires numeric operands, got %T vs %T", op, a, b)
	}
	switch op {
	case "gt":
		return af > bf, nil
	case "gte":
		return af >= bf, nil
	case "lt":
		return af < bf, nil
	case "lte":
		return af <= bf, nil
	}
	return false, fmt.Errorf("unknown numeric operator: %q", op)
}

func containsValue(list interface{}, v interface{}) (bool, error) {
	items, ok := list.([]interface{})
	if !ok {
		return false, fmt.Errorf("in/notIn requires a slice value, got %T", list)
	}
	for _, item := range items {
		if valuesEqual(item, v) {
			return true, nil
		}
	}
	return false, nil
}

