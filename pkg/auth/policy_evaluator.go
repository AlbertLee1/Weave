package auth

import (
	"github.com/liyang/weave/pkg/oms"
)

// PolicyEvaluator decides whether a user can see a single object given a
// fixed set of SecurityPolicies. It is constructed once per call site
// (typically from PolicyFilter) and reused across many objects so the
// per-policy parse cost is amortised.
//
// Semantics:
//
//   - Default deny: if no allow policy matches, the object is hidden.
//   - Deny precedence: if any matching deny policy fires, the object is
//     hidden regardless of allow grants.
//   - PROPERTY-scope policies that match contribute their PropertyMasks
//     to the result; the field list is the union across all matching
//     property policies and is returned only when the object is allowed.
//   - Malformed policy rules are skipped (logged via the validator at
//     write time, ignored at read time so a single bad row cannot break
//     query traffic).
type PolicyEvaluator struct {
	parsed []parsedPolicy
}

// parsedPolicy is the runtime form of a SecurityPolicy: rules already
// unmarshalled and validated, plus the policy type so the evaluator can
// distinguish OBJECT vs PROPERTY scope.
type parsedPolicy struct {
	policyType string
	rules      SecurityPolicyRules
}

// Policy types stored on the SecurityPolicy.PolicyType column. Kept exported
// so callers can use the same constants when constructing fixtures.
const (
	PolicyTypeObject   = "OBJECT"
	PolicyTypeProperty = "PROPERTY"
)

// NewPolicyEvaluator parses and validates the given policies, dropping any
// rows that fail validation. Construction is O(N) in policy count.
func NewPolicyEvaluator(policies []oms.SecurityPolicy) *PolicyEvaluator {
	e := &PolicyEvaluator{
		parsed: make([]parsedPolicy, 0, len(policies)),
	}
	for _, p := range policies {
		rules, err := ParseSecurityPolicyRules(p.Rules)
		if err != nil {
			// Tolerate malformed rows: an evaluator that crashes on a
			// stale row would brick the entire object type. Validation
			// at write time is the primary defence.
			continue
		}
		e.parsed = append(e.parsed, parsedPolicy{
			policyType: p.PolicyType,
			rules:      rules,
		})
	}
	return e
}

// Evaluate returns (allow, propertyMasks, nil) where allow indicates whether
// the user can see the object and propertyMasks lists property API names that
// must be redacted from the wire response.
//
// On deny, the returned mask list is always empty (the caller drops the
// object entirely so masking is moot).
func (e *PolicyEvaluator) Evaluate(user *User, obj map[string]interface{}) (bool, []string, error) {
	if e == nil || len(e.parsed) == 0 {
		// No policies attached -> default deny.
		return false, nil, nil
	}

	allowed := false
	masks := make([]string, 0)
	seenMask := make(map[string]bool)

	for _, p := range e.parsed {
		if !subjectMatches(p.rules.Subjects, user) {
			continue
		}
		if !conditionMatches(p.rules.Condition, obj) {
			continue
		}
		// Subject + condition matched. Apply effect.
		if p.rules.Effect == EffectDeny {
			// Hard deny. Stop immediately so no leftover masks accumulate.
			return false, nil, nil
		}
		// Allow.
		if p.policyType == PolicyTypeObject {
			allowed = true
		}
		if p.policyType == PolicyTypeProperty {
			// Property policies do not by themselves grant object visibility,
			// but their masks DO apply to whoever the OBJECT policies permit.
			for _, m := range p.rules.PropertyMasks {
				if !seenMask[m] {
					seenMask[m] = true
					masks = append(masks, m)
				}
			}
		}
	}

	if !allowed {
		// No object-scope allow grant matched -> default deny. Discard masks.
		return false, nil, nil
	}
	return true, masks, nil
}

// subjectMatches reports whether a SubjectSpec applies to the given user.
// Anonymous=true matches a nil user; otherwise the user's roles or ID must
// intersect the spec's lists.
func subjectMatches(s SubjectSpec, user *User) bool {
	if user == nil {
		return s.Anonymous
	}
	for _, role := range s.Roles {
		for _, ur := range user.Roles {
			if ur == role {
				return true
			}
		}
	}
	for _, id := range s.UserIDs {
		if id == user.ID {
			return true
		}
	}
	return false
}

// conditionMatches walks a ConditionSpec tree against an object's property
// map. Empty Op (zero value) is treated as OpAlways so that policies can
// omit the condition field for "applies to every object".
func conditionMatches(c ConditionSpec, obj map[string]interface{}) bool {
	switch c.Op {
	case "", OpAlways:
		return true
	case OpEquals, OpPropertyEquals:
		got, ok := obj[c.Field]
		if !ok {
			return false
		}
		return valuesEqual(got, c.Value)
	case OpAnd:
		for _, child := range c.Children {
			if !conditionMatches(child, obj) {
				return false
			}
		}
		return true
	case OpOr:
		for _, child := range c.Children {
			if conditionMatches(child, obj) {
				return true
			}
		}
		return false
	case OpNot:
		if len(c.Children) != 1 {
			return false
		}
		return !conditionMatches(c.Children[0], obj)
	}
	// Unknown op (should never happen post-validation): fail closed.
	return false
}

// valuesEqual compares two interface{} values using JSON-style semantics.
// Numbers are normalised to float64 (Go's JSON default) before comparison so
// that an int from a Go fixture matches a float64 from Bleve.
func valuesEqual(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == b
	}
	an, aok := toFloat(a)
	bn, bok := toFloat(b)
	if aok && bok {
		return an == bn
	}
	return a == b
}

// toFloat coerces numeric types to float64 for comparison. Returns false
// for non-numeric inputs (string, bool, etc.) so the caller can fall back
// to direct equality.
func toFloat(v interface{}) (float64, bool) {
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
