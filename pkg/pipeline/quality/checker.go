package quality

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// FKLookup is the narrow caller-supplied surface used by RuleForeignKey.
// Implementations decide what "value exists" means: a SELECT against
// PG, a Bleve term query, a sorted-list lookup over a CSV the upstream
// connector emitted — anything that maps a value to a boolean.
//
// Returning (false, nil) means "value not found"; non-nil err is a
// transport/IO failure and aborts the row.
type FKLookup interface {
	Exists(ctx context.Context, value any) (bool, error)
}

// FKLookupFunc adapts a plain function to FKLookup.
type FKLookupFunc func(ctx context.Context, value any) (bool, error)

// Exists satisfies FKLookup.
func (f FKLookupFunc) Exists(ctx context.Context, value any) (bool, error) {
	return f(ctx, value)
}

// CheckerOptions tunes Checker construction.
type CheckerOptions struct {
	// Rules is the ordered ruleset; evaluation honors declaration
	// order so deterministic violation sequences are reproducible.
	Rules []Rule

	// FKLookups maps Rule.Lookup names to concrete FKLookup
	// implementations. ForeignKey rules referencing an unregistered
	// lookup fail at NewChecker time so misconfiguration surfaces at
	// construction rather than per-row.
	FKLookups map[string]FKLookup

	// PipelineID, RunID, NodeName stamp every emitted Violation.
	// Empty values land as the column DEFAULT '' in PG.
	PipelineID string
	RunID      string
	NodeName   string

	// NowFunc injects the wall clock (matches the SetNowFunc
	// convention in oms.CachedRepository / pkg/oss/computed.Resolver).
	// Zero value defaults to time.Now.
	NowFunc func() time.Time

	// IDFunc generates the violation id; defaults to a 16-byte
	// crypto/rand hex value. Tests inject a deterministic counter so
	// snapshot assertions stay stable.
	IDFunc func() string
}

// Checker evaluates Rules row-by-row and returns the per-row Violation
// slice. Stateful for unique-tracking — call Reset between independent
// runs.
type Checker struct {
	rules     []Rule
	regex     map[string]*regexp.Regexp
	fk        map[string]FKLookup
	uniqueSet map[string]map[string]struct{}

	pipelineID string
	runID      string
	nodeName   string

	now func() time.Time
	id  func() string
}

// NewChecker validates rules + lookup wiring and returns a configured
// Checker. ForeignKey rules referencing an unregistered lookup are
// rejected here so Check never silently returns "missing lookup"
// violations.
func NewChecker(opts CheckerOptions) (*Checker, error) {
	if err := ValidateRules(opts.Rules); err != nil {
		return nil, err
	}
	c := &Checker{
		rules:      append([]Rule(nil), opts.Rules...),
		regex:      make(map[string]*regexp.Regexp),
		fk:         make(map[string]FKLookup),
		uniqueSet:  make(map[string]map[string]struct{}),
		pipelineID: opts.PipelineID,
		runID:      opts.RunID,
		nodeName:   opts.NodeName,
		now:        opts.NowFunc,
		id:         opts.IDFunc,
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.id == nil {
		c.id = randomViolationID
	}
	for _, rule := range c.rules {
		switch rule.Type {
		case RuleRegex:
			rx, err := regexp.Compile(rule.Pattern)
			if err != nil {
				return nil, fmt.Errorf("quality: rule %q: regex compile: %w", rule.Name, err)
			}
			c.regex[rule.Name] = rx
		case RuleForeignKey:
			lookup, ok := opts.FKLookups[rule.Lookup]
			if !ok || lookup == nil {
				return nil, fmt.Errorf("quality: rule %q (foreign_key): lookup %q is not registered", rule.Name, rule.Lookup)
			}
			c.fk[rule.Name] = lookup
		case RuleNotNull, RuleRange, RuleUnique:
			// no per-construction state for the value-only rule
			// types; evalRule handles them entirely from row data.
		}
	}
	return c, nil
}

// Rules returns a defensive copy of the configured ruleset.
func (c *Checker) Rules() []Rule {
	return append([]Rule(nil), c.rules...)
}

// Reset clears the unique-tracking state so the same Checker can be
// reused for a fresh pass without re-allocating the regex/fk maps.
func (c *Checker) Reset() {
	c.uniqueSet = make(map[string]map[string]struct{})
}

// CheckRow evaluates every rule against row at rowIndex and returns
// the resulting Violations (zero-length when the row passes every
// rule). rowKey is an optional caller-supplied trace handle — usually
// a primary key the upstream connector emitted.
//
// Errors from FK lookups abort the row; other rule failures append a
// Violation and continue, so a single row can produce N violations.
func (c *Checker) CheckRow(ctx context.Context, rowIndex int64, rowKey string, row map[string]any) ([]Violation, error) {
	if row == nil {
		row = map[string]any{}
	}
	var violations []Violation
	for _, rule := range c.rules {
		v, err := c.evalRule(ctx, rule, row)
		if err != nil {
			return violations, fmt.Errorf("quality: rule %q at row %d: %w", rule.Name, rowIndex, err)
		}
		if v == nil {
			continue
		}
		violations = append(violations, c.makeViolation(rule, rowIndex, rowKey, v))
	}
	return violations, nil
}

// CheckRows is a convenience wrapper that runs CheckRow over every row
// and returns the concatenated violations. Useful for bounded in-memory
// row slices; large streams should call CheckRow per row instead.
func (c *Checker) CheckRows(ctx context.Context, rows []map[string]any) ([]Violation, error) {
	var out []Violation
	for i, row := range rows {
		vs, err := c.CheckRow(ctx, int64(i), "", row)
		if err != nil {
			return out, err
		}
		out = append(out, vs...)
	}
	return out, nil
}

// ruleViolation captures the failure detail evalRule wants to surface.
// Returned by-value so the rule branches stay branch-by-branch.
type ruleViolation struct {
	reason string
	value  any
}

// evalRule dispatches to the per-type evaluator. Returns nil when the
// rule passes, *ruleViolation when it fails, error on transport failure
// (FK lookup IO).
func (c *Checker) evalRule(ctx context.Context, rule Rule, row map[string]any) (*ruleViolation, error) {
	value, present := row[rule.Field]
	switch rule.Type {
	case RuleNotNull:
		return evalNotNull(rule, value, present), nil
	case RuleRange:
		return evalRange(rule, value, present), nil
	case RuleUnique:
		return c.evalUnique(rule, value, present), nil
	case RuleRegex:
		return c.evalRegex(rule, value, present)
	case RuleForeignKey:
		return c.evalForeignKey(ctx, rule, value, present)
	default:
		return nil, nil
	}
}

func evalNotNull(rule Rule, value any, present bool) *ruleViolation {
	if !present || isNullish(value) {
		return &ruleViolation{reason: fmt.Sprintf("field %q is null or empty", rule.Field), value: value}
	}
	return nil
}

func evalRange(rule Rule, value any, present bool) *ruleViolation {
	if !present || isNullish(value) {
		return &ruleViolation{reason: fmt.Sprintf("field %q is null and rule range requires a value", rule.Field), value: value}
	}
	f, ok := toFloat64(value)
	if !ok {
		return &ruleViolation{reason: fmt.Sprintf("field %q value %v is not numeric", rule.Field, value), value: value}
	}
	if rule.Min != nil && f < *rule.Min {
		return &ruleViolation{reason: fmt.Sprintf("field %q value %v is below min %v", rule.Field, f, *rule.Min), value: value}
	}
	if rule.Max != nil && f > *rule.Max {
		return &ruleViolation{reason: fmt.Sprintf("field %q value %v is above max %v", rule.Field, f, *rule.Max), value: value}
	}
	return nil
}

func (c *Checker) evalUnique(rule Rule, value any, present bool) *ruleViolation {
	if !present || isNullish(value) {
		return nil
	}
	key := stringifyValue(value)
	set, ok := c.uniqueSet[rule.Name]
	if !ok {
		set = make(map[string]struct{})
		c.uniqueSet[rule.Name] = set
	}
	if _, dup := set[key]; dup {
		return &ruleViolation{reason: fmt.Sprintf("field %q value %s repeats", rule.Field, key), value: value}
	}
	set[key] = struct{}{}
	return nil
}

func (c *Checker) evalRegex(rule Rule, value any, present bool) (*ruleViolation, error) {
	if !present || isNullish(value) {
		return &ruleViolation{reason: fmt.Sprintf("field %q is null and rule regex requires a string", rule.Field), value: value}, nil
	}
	s, ok := value.(string)
	if !ok {
		return &ruleViolation{reason: fmt.Sprintf("field %q value is not a string", rule.Field), value: value}, nil
	}
	rx := c.regex[rule.Name]
	if rx == nil {
		return nil, fmt.Errorf("quality: rule %q regex was not compiled", rule.Name)
	}
	if !rx.MatchString(s) {
		return &ruleViolation{reason: fmt.Sprintf("field %q value %q does not match pattern %q", rule.Field, s, rule.Pattern), value: value}, nil
	}
	return nil, nil
}

func (c *Checker) evalForeignKey(ctx context.Context, rule Rule, value any, present bool) (*ruleViolation, error) {
	if !present || isNullish(value) {
		return &ruleViolation{reason: fmt.Sprintf("field %q is null and rule foreign_key requires a value", rule.Field), value: value}, nil
	}
	lookup := c.fk[rule.Name]
	if lookup == nil {
		return nil, errors.New("quality: foreign_key lookup was not wired")
	}
	exists, err := lookup.Exists(ctx, value)
	if err != nil {
		return nil, err
	}
	if !exists {
		return &ruleViolation{reason: fmt.Sprintf("field %q value %v not found in lookup %q", rule.Field, value, rule.Lookup), value: value}, nil
	}
	return nil, nil
}

// makeViolation stamps run-scoped metadata onto rv.
func (c *Checker) makeViolation(rule Rule, rowIndex int64, rowKey string, rv *ruleViolation) Violation {
	return Violation{
		ID:         c.id(),
		PipelineID: c.pipelineID,
		RunID:      c.runID,
		NodeName:   c.nodeName,
		RuleName:   rule.Name,
		RuleType:   rule.Type,
		Field:      rule.Field,
		RowIndex:   rowIndex,
		RowKey:     rowKey,
		Reason:     rv.reason,
		Value:      stringifyValue(rv.value),
		DetectedAt: c.now().UTC(),
	}
}

// isNullish reports whether v should be treated as "missing". Mirrors
// the OMS/Bleve "absent ⇒ skip" convention: nil + empty string + empty
// byte slice all count.
func isNullish(v any) bool {
	if v == nil {
		return true
	}
	switch x := v.(type) {
	case string:
		return x == ""
	case []byte:
		return len(x) == 0
	}
	return false
}

// toFloat64 widens any numeric type into a float64 for range rules.
// Returns (value, false) when v is not numeric. Strings are parsed
// optimistically so CSV-derived rows (which arrive as strings) work
// without a separate "coerce" pass.
func toFloat64(v any) (float64, bool) {
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
	case uint:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case string:
		if x == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// randomViolationID generates a 16-byte hex (~32 char) violation id.
// Defaults for CheckerOptions.IDFunc.
func randomViolationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand should never fail; falling back to "" still
		// produces a usable Violation, the PG INSERT will reject the
		// row instead of silently fabricating an empty PK.
		return ""
	}
	return hex.EncodeToString(b[:])
}
