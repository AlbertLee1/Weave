package masking

import (
	"github.com/liyang/weave/pkg/masking/strategies"
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
		return strategies.Hash(value)
	case MaskRuleRedact:
		return strategies.Redact()
	case MaskRulePartial:
		return strategies.Partial(value)
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
