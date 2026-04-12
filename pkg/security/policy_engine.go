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
)

// Rule is one clause inside a Policy. The JSON tags must match the DSL
// spelled out in the story so that existing JSONB rows decode without
// a schema migration.
type Rule struct {
	Type           RuleType `json:"type"`
	UserAttr       string   `json:"userAttr,omitempty"`
	ObjectProperty string   `json:"objectProperty,omitempty"`
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
type Engine struct {
	mu       sync.RWMutex
	policies map[string][]Policy // keyed by ObjectType RID
}

// NewEngine returns an Engine with no policies registered. Un-policied
// ObjectTypes always compile to a MatchAll query so callers can safely
// AND-combine the result into their pipeline unconditionally.
func NewEngine() *Engine {
	return &Engine{policies: make(map[string][]Policy)}
}

// SetPolicies registers (replacing any previous set) the policies for a given
// ObjectType RID. Subsequent stories will wire PG loading into this method.
func (e *Engine) SetPolicies(objectTypeRID string, policies []Policy) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(policies) == 0 {
		delete(e.policies, objectTypeRID)
		return
	}
	copied := make([]Policy, len(policies))
	copy(copied, policies)
	e.policies[objectTypeRID] = copied
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
	e.mu.RUnlock()

	if len(policies) == 0 {
		return bleve.NewMatchAllQuery(), nil
	}

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
	default:
		return nil, fmt.Errorf("unsupported rule type %q", r.Type)
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
