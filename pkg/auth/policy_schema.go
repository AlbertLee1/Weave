package auth

import (
	"encoding/json"
	"errors"
	"fmt"
)

// SecurityPolicyRules is the formal schema for the JSONB `rules` column on
// security_policies. It is the contract between the admin write path (which
// validates incoming JSON against this schema) and the runtime evaluator
// (which decides allow/deny on every read).
//
// Wire format:
//
//	{
//	  "version": 1,
//	  "effect": "allow" | "deny",
//	  "subjects": { "roles": [...], "userIds": [...], "anonymous": bool },
//	  "condition": { "op": "...", ... },
//	  "propertyMasks": ["fieldA", "fieldB"]
//	}
//
// Version 1 is the only supported wire format. Newer versions can be added
// by branching in ParseSecurityPolicyRules.
type SecurityPolicyRules struct {
	Version       int           `json:"version"`
	Effect        string        `json:"effect"`
	Subjects      SubjectSpec   `json:"subjects"`
	Condition     ConditionSpec `json:"condition,omitempty"`
	PropertyMasks []string      `json:"propertyMasks,omitempty"`
}

// SubjectSpec describes which principals a policy applies to. A subject
// matches if ANY of the listed roles, user IDs, or the Anonymous flag
// satisfies the request. An empty SubjectSpec matches nothing.
type SubjectSpec struct {
	Roles     []string `json:"roles,omitempty"`
	UserIDs   []string `json:"userIds,omitempty"`
	Anonymous bool     `json:"anonymous,omitempty"`
}

// ConditionSpec is the recursive boolean expression evaluated against an
// object's property map. The leaf operators ("always", "propertyEquals",
// "equals") inspect a single field; the combinators ("and", "or", "not")
// chain children together.
//
// Operator catalogue:
//
//	always           -> always true (used as a no-op default)
//	propertyEquals   -> object[Field] == Value
//	equals           -> alias for propertyEquals
//	and              -> all Children true (vacuously true with no children)
//	or               -> any Child true (vacuously false with no children)
//	not              -> exactly one child, inverted
//
// Unknown operators are a hard validation error.
type ConditionSpec struct {
	Op       string          `json:"op"`
	Field    string          `json:"field,omitempty"`
	Value    interface{}     `json:"value,omitempty"`
	Children []ConditionSpec `json:"children,omitempty"`
}

// Allowed effect values.
const (
	EffectAllow = "allow"
	EffectDeny  = "deny"
)

// Allowed condition operators. Kept exported so handlers and clients can
// reference them by name without re-typing string literals.
const (
	OpAlways         = "always"
	OpEquals         = "equals"
	OpPropertyEquals = "propertyEquals"
	OpAnd            = "and"
	OpOr             = "or"
	OpNot            = "not"
)

// ErrInvalidPolicyRules is returned by ValidateSecurityPolicyRules and
// ParseSecurityPolicyRules when the input does not satisfy the schema.
var ErrInvalidPolicyRules = errors.New("invalid security policy rules")

// ParseSecurityPolicyRules unmarshals raw JSON into SecurityPolicyRules and
// validates it. Returns ErrInvalidPolicyRules wrapped with the underlying
// reason on failure.
func ParseSecurityPolicyRules(raw json.RawMessage) (SecurityPolicyRules, error) {
	var r SecurityPolicyRules
	if len(raw) == 0 {
		return r, fmt.Errorf("%w: empty rules", ErrInvalidPolicyRules)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r, fmt.Errorf("%w: %v", ErrInvalidPolicyRules, err)
	}
	if err := ValidateSecurityPolicyRules(r); err != nil {
		return r, err
	}
	return r, nil
}

// ValidateSecurityPolicyRules enforces the structural rules of a parsed
// SecurityPolicyRules value. The runtime evaluator assumes inputs have been
// validated; do NOT skip this step in handlers.
func ValidateSecurityPolicyRules(r SecurityPolicyRules) error {
	if r.Version != 1 {
		return fmt.Errorf("%w: unsupported version %d (must be 1)", ErrInvalidPolicyRules, r.Version)
	}
	if r.Effect != EffectAllow && r.Effect != EffectDeny {
		return fmt.Errorf("%w: effect must be 'allow' or 'deny', got %q", ErrInvalidPolicyRules, r.Effect)
	}
	if len(r.Subjects.Roles) == 0 && len(r.Subjects.UserIDs) == 0 && !r.Subjects.Anonymous {
		return fmt.Errorf("%w: subjects must specify at least one of roles, userIds, or anonymous", ErrInvalidPolicyRules)
	}
	// Condition is optional; an unset Op is treated as OpAlways at evaluate time.
	if r.Condition.Op != "" {
		if err := validateCondition(r.Condition); err != nil {
			return err
		}
	}
	return nil
}

// validateCondition recursively walks the condition tree and rejects unknown
// operators or shape mismatches (e.g. NOT with two children).
func validateCondition(c ConditionSpec) error {
	switch c.Op {
	case OpAlways:
		return nil
	case OpEquals, OpPropertyEquals:
		if c.Field == "" {
			return fmt.Errorf("%w: %s requires a non-empty field", ErrInvalidPolicyRules, c.Op)
		}
		return nil
	case OpAnd, OpOr:
		for _, child := range c.Children {
			if err := validateCondition(child); err != nil {
				return err
			}
		}
		return nil
	case OpNot:
		if len(c.Children) != 1 {
			return fmt.Errorf("%w: not requires exactly one child, got %d", ErrInvalidPolicyRules, len(c.Children))
		}
		return validateCondition(c.Children[0])
	default:
		return fmt.Errorf("%w: unknown condition op %q", ErrInvalidPolicyRules, c.Op)
	}
}
