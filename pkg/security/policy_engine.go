// Package security hosts the row-level and column-level policy engine that
// enforces per-user visibility rules against objects served by OSS.
//
// Scope of this file (US-043 scaffold): in-memory engine, the `eq` rule type,
// and compilation to a Bleve query suitable for AND-combining into the OSS
// query-generation chain. Rule types `in` and `subset`, DB loading, and the
// compiled-query cache land in subsequent stories (US-044..US-046).
//
// The JSON encoding of Rule matches the row shape stored in the existing
// `security_policies.rules` JSONB column; no migration is required.
package security

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
)

// PolicyType mirrors the `policy_type` CHECK constraint on the
// security_policies table. Column-level policies arrive in US-057.
type PolicyType string

const (
	PolicyTypeObject   PolicyType = "OBJECT"
	PolicyTypeProperty PolicyType = "PROPERTY"
)

// RuleType names a mini-DSL rule variant stored inside security_policies.rules.
type RuleType string

const (
	// RuleTypeEq — user.UserAttr must equal object.ObjectProperty.
	RuleTypeEq RuleType = "eq"
	// RuleTypeIn — user.UserAttr (a list of values) overlaps object.ObjectProperty.
	// Compiles to a BooleanQuery whose Should clauses are TermQuery(value,
	// field=objectProperty) for each value the user holds, MinShould=1.
	RuleTypeIn RuleType = "in"
	// RuleTypeMarkingSubset — object.ObjectProperty ⊆ user.markings. User
	// markings are sourced from user.Attributes[userMarkingsKey] (US-059 will
	// populate this from JWT claims). Compiles to the same BooleanQuery shape
	// as RuleTypeIn; for single-valued object marking fields this is exactly
	// the subset semantics, and multi-valued marking fields are deferred to
	// US-058 once the markings_sig index is available.
	RuleTypeMarkingSubset RuleType = "markingSubset"
)

// userMarkingsKey is the user.Attributes key that RuleTypeMarkingSubset reads
// to determine the marking set the caller holds. Kept in one place so US-059
// (JWT injection) and US-058 (OSP merge) write/read the same key.
const userMarkingsKey = "markings"

// MarkingField is the reserved keyword field name used by the Funnel consumer
// to persist each object's marking set into Bleve, and the field the
// auto-marking clause in Evaluate targets when an ObjectType is flagged as
// markings-enabled via SetMarkingsEnabled. Keeping this in one place means
// the indexing pipeline and the policy engine cannot drift.
const MarkingField = "_markings"

// Rule is one clause inside a Policy. The JSON tags must match the DSL
// spelled out in the story so that existing JSONB rows decode without
// a schema migration.
//
// Row-level (OBJECT-scope) policies use Type + UserAttr + ObjectProperty to
// compile into a Bleve query (see compileRule). Column-level (PROPERTY-
// scope) policies use UserAttr + Values + Properties to decide which
// property API names are visible to the caller (see AllowedProperties); the
// Type field is not consulted for PROPERTY-scope rules so existing JSONB
// rows can encode either shape without a type tag collision.
type Rule struct {
	Type           RuleType `json:"type,omitempty"`
	UserAttr       string   `json:"userAttr,omitempty"`
	ObjectProperty string   `json:"objectProperty,omitempty"`

	// Values enumerates the literal user-attribute values that satisfy a
	// PROPERTY-scope rule's guard. The rule applies when the caller's
	// user.Attributes[UserAttr] (string or []string) contains any of these
	// values. Ignored for OBJECT-scope rules.
	Values []string `json:"values,omitempty"`

	// Properties is the list of object property API names granted by a
	// PROPERTY-scope rule when its guard matches. Ignored for OBJECT-scope
	// rules. A rule with an empty UserAttr AND empty Values is an
	// unconditional grant suitable for baseline visibility lists.
	Properties []string `json:"properties,omitempty"`
}

// Policy is one row of the security_policies table. RID / ObjectTypeRID /
// PolicyType map 1:1 to the corresponding columns; Rules is the decoded
// JSONB payload.
type Policy struct {
	RID           string     `json:"rid"`
	ObjectTypeRID string     `json:"objectTypeRid"`
	PolicyType    PolicyType `json:"policyType"`
	Rules         []Rule     `json:"rules"`
}

// Engine compiles the row-level policies registered for an ObjectType into
// a Bleve query. The Evaluate result is intended to be AND-combined by OSS
// Load / Search / Aggregate into their own query pipelines.
//
// An optional PolicyCache (attached via SetCache) memoises compiled queries
// per (userID, objectTypeRID, policyVersion). SetPolicies bumps the per-RID
// version and invalidates cached entries so stale results never serve.
type Engine struct {
	mu       sync.RWMutex
	policies map[string][]Policy // keyed by ObjectType RID
	versions map[string]int64    // keyed by ObjectType RID; bumped on SetPolicies
	cache    *PolicyCache
	// markingsEnabled is the set of ObjectType RIDs for which Evaluate must
	// auto-append a marking-subset clause against MarkingField. Populated via
	// SetMarkingsEnabled so callers (main.go, tests) can turn the feature on
	// per ObjectType without authoring a synthetic Policy row.
	markingsEnabled map[string]struct{}
}

// NewEngine returns an Engine with no policies registered. Un-policied
// ObjectTypes always compile to a MatchAll query so callers can safely
// AND-combine the result into their pipeline unconditionally.
func NewEngine() *Engine {
	return &Engine{
		policies:        make(map[string][]Policy),
		versions:        make(map[string]int64),
		markingsEnabled: make(map[string]struct{}),
	}
}

// SetMarkingsEnabled toggles auto-marking enforcement for an ObjectType RID.
// Passing enabled=true registers the RID so every subsequent Evaluate call
// AND-combines a marking-subset clause against MarkingField on top of any
// explicit OBJECT-scope policies. Passing enabled=false removes the RID.
//
// The per-RID version counter is bumped on every call so an attached
// PolicyCache drops stale compiled queries. Safe for concurrent use.
func (e *Engine) SetMarkingsEnabled(objectTypeRID string, enabled bool) {
	e.mu.Lock()
	if enabled {
		e.markingsEnabled[objectTypeRID] = struct{}{}
	} else {
		delete(e.markingsEnabled, objectTypeRID)
	}
	e.versions[objectTypeRID]++
	cache := e.cache
	e.mu.Unlock()
	if cache != nil {
		cache.InvalidateObjectType(objectTypeRID)
	}
}

// MarkingsEnabled reports whether SetMarkingsEnabled has registered the
// given ObjectType RID for auto-marking enforcement.
func (e *Engine) MarkingsEnabled(objectTypeRID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.markingsEnabled[objectTypeRID]
	return ok
}

// SetCache attaches a PolicyCache for compiled-query memoisation. Passing nil
// disables caching. Safe to call at runtime, but existing entries under the
// previous cache are not carried over.
func (e *Engine) SetCache(c *PolicyCache) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cache = c
}

// PolicyVersion returns the current version counter for an ObjectType RID.
// Un-policied RIDs return 0. Callers can combine this with PolicyCache.Get to
// perform a standalone lookup outside the Engine.Evaluate fast path.
func (e *Engine) PolicyVersion(objectTypeRID string) int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.versions[objectTypeRID]
}

// SetPolicies registers (replacing any previous set) the policies for a given
// ObjectType RID, bumps the per-RID version counter, and drops any attached
// cache entries for that RID so stale compiled queries never serve again.
func (e *Engine) SetPolicies(objectTypeRID string, policies []Policy) {
	e.mu.Lock()
	if len(policies) == 0 {
		delete(e.policies, objectTypeRID)
	} else {
		copied := make([]Policy, len(policies))
		copy(copied, policies)
		e.policies[objectTypeRID] = copied
	}
	e.versions[objectTypeRID]++
	cache := e.cache
	e.mu.Unlock()
	if cache != nil {
		cache.InvalidateObjectType(objectTypeRID)
	}
}

// Evaluate compiles every OBJECT-typed policy registered for ot into a single
// Bleve query.
//
//   - Zero policies → bleve.NewMatchAllQuery()
//   - A policy rule that references a user attribute the caller lacks →
//     bleve.NewMatchNoneQuery() (fail-closed)
//   - N>1 rules / policies → bleve.NewConjunctionQuery(...)
func (e *Engine) Evaluate(ctx context.Context, user *auth.User, ot oms.ObjectType) (query.Query, error) {
	_ = ctx

	e.mu.RLock()
	policies := e.policies[ot.RID]
	version := e.versions[ot.RID]
	cache := e.cache
	_, markingsOn := e.markingsEnabled[ot.RID]
	e.mu.RUnlock()

	var userID string
	if user != nil {
		userID = user.ID
	}

	if cache != nil && userID != "" {
		if cached, ok := cache.Get(userID, ot.RID, version); ok {
			return cached, nil
		}
	}

	q, err := compilePolicies(policies, user, markingsOn)
	if err != nil {
		return nil, err
	}

	if cache != nil && userID != "" {
		cache.Put(userID, ot.RID, version, q)
	}
	return q, nil
}

// AllowedProperties returns the set of object property API names that the
// caller is permitted to see on ot. The return convention is:
//
//   - nil slice  → no PROPERTY-scope policy registered; callers should treat
//     this as "all properties allowed" and skip wire-level filtering so the
//     un-policied back-compat path continues to return full property payloads.
//   - non-nil slice (including empty) → explicit allow list; any property API
//     name not present MUST be dropped from the serialized WireObject
//     (omitted, not nulled).
//
// Evaluation walks every PROPERTY-typed policy registered for ot.RID. For
// each rule whose guard applies to the caller, the rule's Properties are
// added to the running union. A rule with empty UserAttr AND empty Values
// is an unconditional grant (baseline visibility). When UserAttr is set,
// the caller's user.Attributes[UserAttr] (string OR []string) must contain
// at least one of the Values for the grant to apply — or, when Values is
// empty, the attribute just needs to be present.
//
// Security note: the engine fail-closes per RULE, not per POLICY — a rule
// whose guard misses contributes nothing, but other rules in the same
// policy still run. This matches the "union of matching grants" semantics
// that column-level ABAC typically ships with and keeps the data-owner
// mental model simple (each rule is an additive grant).
func (e *Engine) AllowedProperties(ctx context.Context, user *auth.User, ot oms.ObjectType) []string {
	_ = ctx

	e.mu.RLock()
	policies := e.policies[ot.RID]
	e.mu.RUnlock()

	var hasPropertyPolicy bool
	grants := make(map[string]struct{})
	for _, p := range policies {
		if p.PolicyType != PolicyTypeProperty {
			continue
		}
		hasPropertyPolicy = true
		for _, r := range p.Rules {
			if !propertyRuleMatches(r, user) {
				continue
			}
			for _, prop := range r.Properties {
				if prop == "" {
					continue
				}
				grants[prop] = struct{}{}
			}
		}
	}

	if !hasPropertyPolicy {
		return nil
	}

	out := make([]string, 0, len(grants))
	for k := range grants {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// propertyRuleMatches decides whether a PROPERTY-scope rule applies to the
// caller. Semantics:
//
//   - Empty UserAttr AND empty Values → unconditional grant (always matches).
//   - Empty UserAttr AND non-empty Values → invalid guard, never matches
//     (fail closed; validators should reject this at write time).
//   - UserAttr set, empty Values → match whenever the caller has any
//     non-empty value under that attribute key (presence check).
//   - UserAttr set, non-empty Values → match when any value the caller
//     holds under that attribute is also listed in Values.
func propertyRuleMatches(r Rule, user *auth.User) bool {
	if r.UserAttr == "" {
		return len(r.Values) == 0
	}
	if s, ok := userAttrString(user, r.UserAttr); ok {
		if len(r.Values) == 0 {
			return true
		}
		for _, v := range r.Values {
			if v == s {
				return true
			}
		}
		return false
	}
	if list, ok := userAttrStringSlice(user, r.UserAttr); ok {
		if len(r.Values) == 0 {
			return true
		}
		for _, v := range list {
			for _, w := range r.Values {
				if v == w {
					return true
				}
			}
		}
	}
	return false
}

// compilePolicies runs the DSL compiler over every OBJECT-typed policy in
// policies. Extracted from Evaluate so cached results can short-circuit the
// compile pass without duplicating fail-closed / fallthrough logic.
//
// markingsEnabled=true appends a synthetic RuleTypeMarkingSubset clause
// against MarkingField so ObjectTypes registered via SetMarkingsEnabled
// inherit marking enforcement without a persisted policy row.
func compilePolicies(policies []Policy, user *auth.User, markingsEnabled bool) (query.Query, error) {
	var clauses []query.Query
	for _, p := range policies {
		if p.PolicyType != "" && p.PolicyType != PolicyTypeObject {
			continue
		}
		for _, r := range p.Rules {
			q, err := compileRule(r, user)
			if err != nil {
				return nil, fmt.Errorf("policy %s: %w", p.RID, err)
			}
			if q == nil {
				continue
			}
			if _, deny := q.(*query.MatchNoneQuery); deny {
				return q, nil
			}
			clauses = append(clauses, q)
		}
	}

	if markingsEnabled {
		mq, err := compileRule(Rule{
			Type:           RuleTypeMarkingSubset,
			ObjectProperty: MarkingField,
		}, user)
		if err != nil {
			return nil, fmt.Errorf("auto marking clause: %w", err)
		}
		if _, deny := mq.(*query.MatchNoneQuery); deny {
			return mq, nil
		}
		if mq != nil {
			clauses = append(clauses, mq)
		}
	}

	switch len(clauses) {
	case 0:
		return bleve.NewMatchAllQuery(), nil
	case 1:
		return clauses[0], nil
	default:
		return bleve.NewConjunctionQuery(clauses...), nil
	}
}

// compileRule turns a single Rule into a Bleve query clause. Unknown rule
// types surface as errors so US-044 can extend this switch without silently
// degrading to "allow all".
func compileRule(r Rule, user *auth.User) (query.Query, error) {
	switch r.Type {
	case RuleTypeEq:
		if r.UserAttr == "" || r.ObjectProperty == "" {
			return nil, fmt.Errorf("eq rule requires userAttr and objectProperty")
		}
		val, ok := userAttrString(user, r.UserAttr)
		if !ok {
			return bleve.NewMatchNoneQuery(), nil
		}
		tq := bleve.NewTermQuery(val)
		tq.SetField(r.ObjectProperty)
		return tq, nil
	case RuleTypeIn:
		if r.UserAttr == "" || r.ObjectProperty == "" {
			return nil, fmt.Errorf("in rule requires userAttr and objectProperty")
		}
		values, ok := userAttrStringSlice(user, r.UserAttr)
		if !ok || len(values) == 0 {
			return bleve.NewMatchNoneQuery(), nil
		}
		return buildShouldTermsQuery(values, r.ObjectProperty), nil
	case RuleTypeMarkingSubset:
		if r.ObjectProperty == "" {
			return nil, fmt.Errorf("markingSubset rule requires objectProperty")
		}
		markings, ok := userAttrStringSlice(user, userMarkingsKey)
		if !ok || len(markings) == 0 {
			return bleve.NewMatchNoneQuery(), nil
		}
		return buildShouldTermsQuery(markings, r.ObjectProperty), nil
	default:
		return nil, fmt.Errorf("unsupported rule type %q", r.Type)
	}
}

// buildShouldTermsQuery returns a BooleanQuery whose Should clauses are one
// TermQuery per value against the given field, MinShould=1. The returned
// query matches any doc whose field contains at least one of the values;
// for single-valued fields that is exactly set-membership, for multi-valued
// fields it is set-intersection (see RuleTypeIn / RuleTypeMarkingSubset).
func buildShouldTermsQuery(values []string, field string) query.Query {
	bq := bleve.NewBooleanQuery()
	shoulds := make([]query.Query, 0, len(values))
	for _, v := range values {
		tq := bleve.NewTermQuery(v)
		tq.SetField(field)
		shoulds = append(shoulds, tq)
	}
	bq.AddShould(shoulds...)
	bq.SetMinShould(1)
	return bq
}

// userAttrStringSlice fetches a list-shaped user attribute. Returns (_, false)
// when the user is nil, the Attributes map is nil, the key is absent, or the
// stored value is neither a []string nor a []any of strings. A single string
// value is tolerated and wrapped in a length-1 slice so callers (RuleTypeIn)
// don't have to special-case scalar JWT claims that only occasionally hold
// multiple values.
func userAttrStringSlice(user *auth.User, key string) ([]string, bool) {
	if user == nil || user.Attributes == nil {
		return nil, false
	}
	raw, ok := user.Attributes[key]
	if !ok {
		return nil, false
	}
	switch v := raw.(type) {
	case []string:
		if len(v) == 0 {
			return nil, false
		}
		out := make([]string, 0, len(v))
		for _, s := range v {
			if s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	case []any:
		if len(v) == 0 {
			return nil, false
		}
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	case string:
		if v == "" {
			return nil, false
		}
		return []string{v}, true
	default:
		return nil, false
	}
}

// userAttrString fetches a string-shaped user attribute. Returns (_, false)
// when the user is nil, the Attributes map is nil, the key is absent, or the
// stored value is not representable as a string.
func userAttrString(user *auth.User, key string) (string, bool) {
	if user == nil || user.Attributes == nil {
		return "", false
	}
	raw, ok := user.Attributes[key]
	if !ok {
		return "", false
	}
	switch v := raw.(type) {
	case string:
		if v == "" {
			return "", false
		}
		return v, true
	case fmt.Stringer:
		s := v.String()
		if s == "" {
			return "", false
		}
		return s, true
	default:
		return "", false
	}
}
