package contract

import (
	"fmt"
	"regexp"
	"strconv"
)

// MatchBody compares two JSON-decoded values for contract-test purposes.
//
// Default semantics: walk expected; for every leaf path, the actual value at
// the same path must deep-equal it. Maps in actual are allowed to carry
// EXTRA keys not declared in expected (forward compatibility) unless strict
// is true. Matcher rules override comparison at the listed paths.
//
// Returns a slice of error messages — one per detected mismatch — so callers
// can surface every drift in one report instead of dropping out on the first.
func MatchBody(expected, actual interface{}, matchers map[string]MatcherRule, strict bool) []error {
	var errs []error
	walkAndMatch("$", expected, actual, matchers, strict, &errs)
	return errs
}

func walkAndMatch(path string, expected, actual interface{}, matchers map[string]MatcherRule, strict bool, errs *[]error) {
	if rule, ok := matchers[path]; ok {
		if e := applyMatcher(path, rule, actual); e != nil {
			*errs = append(*errs, e)
		}
		return
	}
	if expected == nil {
		if actual != nil {
			*errs = append(*errs, fmt.Errorf("at %s: expected null, got %v", path, actual))
		}
		return
	}
	switch exp := expected.(type) {
	case map[string]interface{}:
		actMap, ok := actual.(map[string]interface{})
		if !ok {
			*errs = append(*errs, fmt.Errorf("at %s: expected object, got %T", path, actual))
			return
		}
		for k, v := range exp {
			child := joinPath(path, k)
			actVal, present := actMap[k]
			if !present {
				if rule, ruleOK := matchers[child]; ruleOK && rule.Match == "ignore" {
					continue
				}
				*errs = append(*errs, fmt.Errorf("at %s: expected key missing from actual", child))
				continue
			}
			walkAndMatch(child, v, actVal, matchers, strict, errs)
		}
		if strict {
			for k := range actMap {
				if _, declared := exp[k]; !declared {
					*errs = append(*errs, fmt.Errorf("at %s: actual has unexpected key %q (strict mode)", path, k))
				}
			}
		}
	case []interface{}:
		actSlice, ok := actual.([]interface{})
		if !ok {
			*errs = append(*errs, fmt.Errorf("at %s: expected array, got %T", path, actual))
			return
		}
		if len(exp) != len(actSlice) {
			*errs = append(*errs, fmt.Errorf("at %s: array length mismatch (expected %d, got %d)", path, len(exp), len(actSlice)))
			return
		}
		for i, v := range exp {
			child := joinPath(path, strconv.Itoa(i))
			walkAndMatch(child, v, actSlice[i], matchers, strict, errs)
		}
	default:
		if !scalarEqual(exp, actual) {
			*errs = append(*errs, fmt.Errorf("at %s: expected %v (%T), got %v (%T)", path, exp, exp, actual, actual))
		}
	}
}

func applyMatcher(path string, rule MatcherRule, actual interface{}) error {
	switch rule.Match {
	case "", "exact":
		// "exact" inside an explicit rule still means deep-equal at this path —
		// rare in practice but documented.
		if !scalarEqual(rule.Value, actual) {
			return fmt.Errorf("at %s: expected %v, got %v", path, rule.Value, actual)
		}
		return nil
	case "type":
		want, _ := rule.Value.(string)
		if !jsonTypeOK(actual, want) {
			return fmt.Errorf("at %s: expected type %q, got %q", path, want, jsonType(actual))
		}
		return nil
	case "regex":
		pattern, _ := rule.Value.(string)
		re, err := regexp.Compile(pattern)
		if err != nil {
			return fmt.Errorf("at %s: invalid regex %q: %v", path, pattern, err)
		}
		s, ok := actual.(string)
		if !ok {
			return fmt.Errorf("at %s: regex matcher requires string, got %T", path, actual)
		}
		if !re.MatchString(s) {
			return fmt.Errorf("at %s: %q does not match regex %q", path, s, pattern)
		}
		return nil
	case "presence":
		// Reaching applyMatcher means walkAndMatch found the key — that IS the
		// presence assertion. (Missing keys never call applyMatcher; the parent
		// loop emits a "key missing" error instead.) Null counts as present.
		return nil
	case "ignore":
		return nil
	default:
		return fmt.Errorf("at %s: unknown matcher type %q", path, rule.Match)
	}
}

// scalarEqual compares two scalar (or root-of-deep-tree) values using
// json-shape semantics. JSON numbers always decode to float64 so we lean on
// that, but we accept int/int64 sources too for callers building expected
// values from Go literals.
func scalarEqual(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if af, ok := toFloat64(a); ok {
		if bf, ok := toFloat64(b); ok {
			return af == bf
		}
		return false
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b) && fmt.Sprintf("%T", a) == fmt.Sprintf("%T", b)
}

func toFloat64(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func jsonType(v interface{}) string {
	if v == nil {
		return "null"
	}
	switch n := v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		if n == float64(int64(n)) {
			return "integer"
		}
		return "number"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	}
	return fmt.Sprintf("%T", v)
}

// jsonTypeOK distinguishes integer (whole number) from number (any). The
// matcher's "number" rule accepts both whole and fractional; "integer" insists
// on whole.
func jsonTypeOK(actual interface{}, want string) bool {
	got := jsonType(actual)
	if got == want {
		return true
	}
	// "integer" is a subtype of "number"
	if want == "number" && got == "integer" {
		return true
	}
	return false
}

// joinPath appends a key/index segment to a `$.a.b` style path. Leading `$`
// (root sentinel) is preserved without a trailing dot for the "$" case.
func joinPath(base, segment string) string {
	if base == "" || base == "$" {
		return "$." + segment
	}
	return base + "." + segment
}
