package masking

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// ApplyMaskRule returns the value rewritten according to rule. The transform
// works on any JSON-decoded value: strings are rewritten directly, other
// primitives (numbers, bools) are stringified first. nil inputs short-circuit
// to nil so downstream callers do not need to pre-filter empty properties.
//
// Unknown rules pass the input through unchanged so a future rule addition
// never panics in production against a stale cached policy; the validator
// has already rejected unknown rules at the admin CRUD surface.
func ApplyMaskRule(rule MaskRule, value interface{}) interface{} {
	if value == nil {
		return nil
	}
	switch rule {
	case MaskRuleHash:
		return hashValue(value)
	case MaskRuleRedact:
		return "[REDACTED]"
	case MaskRulePartial:
		return partialValue(value)
	default:
		return value
	}
}

// ApplyTransforms mutates props in place, rewriting each key listed in
// transforms with the corresponding mask rule. Keys not present in transforms
// are untouched; keys in transforms but not in props are ignored (nil values
// stay nil).
func ApplyTransforms(props map[string]interface{}, transforms map[string]MaskRule) {
	if len(props) == 0 || len(transforms) == 0 {
		return
	}
	for k, rule := range transforms {
		if _, ok := props[k]; !ok {
			continue
		}
		props[k] = ApplyMaskRule(rule, props[k])
	}
}

func hashValue(v interface{}) string {
	s := toString(v)
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// partialValue reveals the first and last character of the value and replaces
// everything in between with asterisks. Strings of length <= 2 get fully
// masked so no source byte leaks. Non-string inputs are stringified first.
func partialValue(v interface{}) interface{} {
	s := toString(v)
	if s == "" {
		return ""
	}
	if len(s) <= 2 {
		return strings.Repeat("*", len(s))
	}
	return s[:1] + strings.Repeat("*", len(s)-2) + s[len(s)-1:]
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
