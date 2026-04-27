package logic

import (
	"fmt"
	"strconv"
	"strings"
)

// substituteVars rewrites every `{{path}}` placeholder in s with its
// looked-up state value. Unknown paths render to an empty string.
// Values are coerced to strings via fmt.Sprint so callers can splice
// numbers / bools into prompts without bespoke handling.
func substituteVars(s string, state map[string]any) string {
	if s == "" || !strings.Contains(s, "{{") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	i := 0
	for i < len(s) {
		if i+1 < len(s) && s[i] == '{' && s[i+1] == '{' {
			end := strings.Index(s[i+2:], "}}")
			if end < 0 {
				out.WriteString(s[i:])
				break
			}
			key := strings.TrimSpace(s[i+2 : i+2+end])
			val, ok := lookupPath(state, key)
			if ok {
				out.WriteString(stringify(val))
			}
			i = i + 2 + end + 2
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// substituteVarsMap walks a JSON-shaped value and substitutes
// placeholders inside every string leaf. Used by the tool node to fan
// `{{...}}` substitution into request param maps without forcing
// callers to flatten the whole shape into one string.
func substituteVarsMap(in map[string]any, state map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = substituteVarsAny(v, state)
	}
	return out
}

func substituteVarsAny(v any, state map[string]any) any {
	switch x := v.(type) {
	case string:
		return substituteVars(x, state)
	case map[string]any:
		return substituteVarsMap(x, state)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = substituteVarsAny(e, state)
		}
		return out
	default:
		return v
	}
}

// lookupPath reads a dotted-path value from a JSON-shaped state map.
// "n1.content" on {n1: {content: "hi"}} returns "hi", true. Missing
// segments return "", false.
func lookupPath(state map[string]any, path string) (any, bool) {
	if path == "" || state == nil {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var cur any = state
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, exists := m[p]
		if !exists {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// stringify renders v as a string for prompt-template substitution.
func stringify(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

// evalCondition evaluates a tiny three-token mini-DSL of the form
// `<lhs> <op> <rhs>` where op is one of ==, !=, <, <=, >, >=, contains.
// Both sides are interpreted as either a quoted string, a number, a
// bool, or a bareword (treated as a literal string). The function only
// supports a single comparison; chained / parenthesised expressions are
// out of scope for v1.
func evalCondition(cond string) (bool, error) {
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return false, fmt.Errorf("empty condition")
	}
	// Special case: a bare bool / non-zero literal evaluates directly.
	if cond == "true" {
		return true, nil
	}
	if cond == "false" {
		return false, nil
	}
	op, lhs, rhs, ok := splitCondition(cond)
	if !ok {
		return false, fmt.Errorf("malformed condition %q (expected lhs <op> rhs)", cond)
	}
	lv := parseScalar(lhs)
	rv := parseScalar(rhs)
	return compareScalars(lv, op, rv)
}

// splitCondition isolates the operator and the two operands.
func splitCondition(s string) (op, lhs, rhs string, ok bool) {
	// Order matters: longer operators must be checked before their
	// single-char prefixes. " contains " uses spaces to disambiguate
	// from a substring inside a quoted operand.
	ops := []string{"==", "!=", "<=", ">=", " contains ", "<", ">"}
	for _, candidate := range ops {
		idx := indexOutsideQuotes(s, candidate)
		if idx < 0 {
			continue
		}
		op = strings.TrimSpace(candidate)
		lhs = strings.TrimSpace(s[:idx])
		rhs = strings.TrimSpace(s[idx+len(candidate):])
		if lhs == "" || rhs == "" {
			return "", "", "", false
		}
		return op, lhs, rhs, true
	}
	return "", "", "", false
}

// indexOutsideQuotes finds substr in s, ignoring matches that fall
// inside a single- or double-quoted region. Returns -1 if not found.
func indexOutsideQuotes(s, substr string) int {
	inSingle, inDouble := false, false
	for i := 0; i+len(substr) <= len(s); i++ {
		c := s[i]
		switch c {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		}
		if inSingle || inDouble {
			continue
		}
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// parseScalar coerces a token into either a number, a bool, or a string.
func parseScalar(tok string) any {
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return ""
	}
	if (strings.HasPrefix(tok, "\"") && strings.HasSuffix(tok, "\"")) ||
		(strings.HasPrefix(tok, "'") && strings.HasSuffix(tok, "'")) {
		return tok[1 : len(tok)-1]
	}
	if tok == "true" {
		return true
	}
	if tok == "false" {
		return false
	}
	if n, err := strconv.ParseFloat(tok, 64); err == nil {
		return n
	}
	return tok
}

// compareScalars applies op to the two scalar operands. Numeric ops
// require both sides to coerce to a float; equality uses the loose
// "stringify and compare" rule so number/string interop just works.
func compareScalars(lhs any, op string, rhs any) (bool, error) {
	switch op {
	case "==":
		return stringify(lhs) == stringify(rhs), nil
	case "!=":
		return stringify(lhs) != stringify(rhs), nil
	case "contains":
		return strings.Contains(stringify(lhs), stringify(rhs)), nil
	}
	lf, lok := toFloat(lhs)
	rf, rok := toFloat(rhs)
	if !lok || !rok {
		return false, fmt.Errorf("operator %q requires numeric operands", op)
	}
	switch op {
	case "<":
		return lf < rf, nil
	case "<=":
		return lf <= rf, nil
	case ">":
		return lf > rf, nil
	case ">=":
		return lf >= rf, nil
	}
	return false, fmt.Errorf("unsupported operator %q", op)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}
