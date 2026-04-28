// Package quality implements the data-quality rule DSL and runtime for
// pipeline rows (US-296). A Rule describes one expectation about the
// shape or value of a column ("this field is never null", "amount is
// between 0 and 1e9", "user_id exists in the users table"); a Checker
// evaluates a sequence of Rules row-by-row and emits a Violation for
// every row that fails. Violations land in the quality_violations
// table so operators can audit data hygiene over time.
//
// The package is connector-agnostic: callers feed it
// map[string]any rows from any source (CSV, JDBC, Kafka, in-memory
// tests). Rule evaluation is pure-Go (regexp/regexp + reflect-free
// type checks) so it stays inside the CGO_ENABLED=0 invariant from
// US-230.
//
// Five rule types ship in v1, named per the PRD:
//
//	notNull     — value is present AND non-empty AND non-zero per type
//	range       — numeric value falls within [min, max] (inclusive)
//	unique      — value is unique across all rows seen so far for the
//	              same Rule (Checker tracks per-rule seen sets)
//	regex       — value matches the compiled pattern (string fields
//	              only; non-string values fail)
//	foreign_key — value exists in a named lookup the caller registers
//	              (cross-table referential integrity)
package quality

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// RuleType enumerates the supported quality-rule kinds. Wire values
// match the PRD acceptance criteria verbatim — including the
// snake_case "foreign_key" outlier — so admin-authored YAML/JSON keeps
// the same identifiers as the user story.
type RuleType string

const (
	RuleNotNull    RuleType = "notNull"
	RuleRange      RuleType = "range"
	RuleUnique     RuleType = "unique"
	RuleRegex      RuleType = "regex"
	RuleForeignKey RuleType = "foreign_key"
)

// AllRuleTypes lists every supported rule type in declaration order.
// Useful for validation lists and admin-UI dropdowns.
func AllRuleTypes() []RuleType {
	return []RuleType{RuleNotNull, RuleRange, RuleUnique, RuleRegex, RuleForeignKey}
}

// IsKnownRuleType reports whether t is one of the supported types.
func IsKnownRuleType(t RuleType) bool {
	for _, kt := range AllRuleTypes() {
		if t == kt {
			return true
		}
	}
	return false
}

// Rule is one declarative quality expectation.
//
// Name is operator-assigned and must be unique within a ruleset; it
// rides on every Violation so dashboards can group failures. Type
// selects the evaluation branch. Field is the target column for
// per-field rules; future row-level rules may leave it empty.
//
// Type-specific fields:
//
//	Min, Max — RuleRange. Both pointers so absent=unbounded; Min<=Max
//	           is enforced at validate time when both are set.
//	Pattern  — RuleRegex source. Compiled at NewChecker time.
//	Lookup   — RuleForeignKey lookup name; the caller registers the
//	           matching FKLookup on CheckerOptions.FKLookups.
//
// Description is a free-form human note shown in error messages and
// admin UIs; never load-bearing for the runtime.
type Rule struct {
	Name        string   `json:"name"`
	Type        RuleType `json:"type"`
	Field       string   `json:"field,omitempty"`
	Description string   `json:"description,omitempty"`

	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Pattern string   `json:"pattern,omitempty"`
	Lookup  string   `json:"lookup,omitempty"`
}

// ruleNameRE matches operator-assigned rule names. Same shape as node
// names in pkg/pipeline so admin-authored YAML round-trips cleanly.
var ruleNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// Validate checks the rule is structurally well-formed. Compiled
// regexes are NOT exercised here — the Checker compiles them once at
// construction time and caches the result.
func (r Rule) Validate() error {
	if r.Name == "" {
		return errors.New("quality: rule name must not be empty")
	}
	if !ruleNameRE.MatchString(r.Name) {
		return fmt.Errorf("quality: rule name %q is invalid: must match %s", r.Name, ruleNameRE.String())
	}
	if !IsKnownRuleType(r.Type) {
		return fmt.Errorf("quality: rule %q has unknown type %q (allowed: %v)", r.Name, r.Type, AllRuleTypes())
	}
	if r.requiresField() && strings.TrimSpace(r.Field) == "" {
		return fmt.Errorf("quality: rule %q (%s) requires field", r.Name, r.Type)
	}
	return r.validateTypeSpecific()
}

// validateTypeSpecific dispatches to the per-type sanity check. Pulled
// out of Validate so its cyclomatic complexity stays under the lint
// floor.
func (r Rule) validateTypeSpecific() error {
	switch r.Type {
	case RuleNotNull, RuleUnique:
		return nil
	case RuleRange:
		return r.validateRange()
	case RuleRegex:
		return r.validateRegex()
	case RuleForeignKey:
		return r.validateForeignKey()
	default:
		return nil
	}
}

func (r Rule) validateRange() error {
	if r.Min == nil && r.Max == nil {
		return fmt.Errorf("quality: rule %q (range) requires min and/or max", r.Name)
	}
	if r.Min != nil && r.Max != nil && *r.Min > *r.Max {
		return fmt.Errorf("quality: rule %q (range) min=%v exceeds max=%v", r.Name, *r.Min, *r.Max)
	}
	return nil
}

func (r Rule) validateRegex() error {
	if strings.TrimSpace(r.Pattern) == "" {
		return fmt.Errorf("quality: rule %q (regex) requires pattern", r.Name)
	}
	if _, err := regexp.Compile(r.Pattern); err != nil {
		return fmt.Errorf("quality: rule %q (regex) pattern invalid: %w", r.Name, err)
	}
	return nil
}

func (r Rule) validateForeignKey() error {
	if strings.TrimSpace(r.Lookup) == "" {
		return fmt.Errorf("quality: rule %q (foreign_key) requires lookup", r.Name)
	}
	return nil
}

// requiresField reports whether r.Field is mandatory. All v1 rules are
// per-column so this is a constant true today; kept as a method so a
// future row-level rule type can opt out without touching Validate's
// caller graph.
func (r Rule) requiresField() bool {
	switch r.Type {
	case RuleNotNull, RuleRange, RuleUnique, RuleRegex, RuleForeignKey:
		return true
	}
	return false
}

// ValidateRules checks every rule individually and rejects duplicate
// names. Centralized so handlers / Checker constructors share one
// preflight path.
func ValidateRules(rules []Rule) error {
	seen := make(map[string]struct{}, len(rules))
	for i, rule := range rules {
		if err := rule.Validate(); err != nil {
			return fmt.Errorf("rules[%d]: %w", i, err)
		}
		if _, dup := seen[rule.Name]; dup {
			return fmt.Errorf("rules[%d]: duplicate rule name %q", i, rule.Name)
		}
		seen[rule.Name] = struct{}{}
	}
	return nil
}
