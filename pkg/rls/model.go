// Package rls implements US-256 row-level security: per-(ObjectType, user-
// scope) predicate filters that are AND-combined into read paths so denied
// rows never materialise. A RowPolicy carries three things: the ObjectType it
// applies to, the where-clause predicate the row must satisfy, and the
// AppliesTo scope (roles/groups/users) that decides which callers the policy
// governs. Multiple policies applicable to the same caller on the same
// ObjectType are OR-combined — a row is visible if any applicable predicate
// matches. ObjectTypes with zero applicable policies flow through unchanged.
package rls

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// Sentinel errors surfaced by Validate and by the Store implementations.
var (
	ErrObjectTypeRIDRequired = errors.New("objectTypeRID is required")
	ErrPredicateRequired     = errors.New("predicate is required")
	ErrNotFound              = errors.New("row policy not found")
)

// AppliesTo decides which callers a RowPolicy governs. A caller matches when
// ANY of the three dimensions (Roles/Groups/Users) overlap with the caller's
// identity. Empty AppliesTo matches nobody.
type AppliesTo struct {
	Roles  []string `json:"roles,omitempty"`
	Groups []string `json:"groups,omitempty"`
	Users  []string `json:"users,omitempty"`
}

// IsApplicable reports whether the policy scope covers the given caller.
// userGroups should carry the group names resolved from GroupMembershipLookup.
func (a AppliesTo) IsApplicable(user *auth.User, userGroups []string) bool {
	if user == nil {
		return false
	}
	if len(a.Roles) > 0 {
		for _, r := range a.Roles {
			for _, ur := range user.Roles {
				if r == ur {
					return true
				}
			}
		}
	}
	if len(a.Groups) > 0 && len(userGroups) > 0 {
		for _, g := range a.Groups {
			for _, ug := range userGroups {
				if g == ug {
					return true
				}
			}
		}
	}
	if len(a.Users) > 0 {
		for _, u := range a.Users {
			if u == "" {
				continue
			}
			if u == user.ID || u == user.Email {
				return true
			}
		}
	}
	return false
}

// RowPolicy is one row of the row_policies table. Predicate is a
// pkg/oss/where.WhereClause serialised as JSON; callers that need a typed
// view can json.Unmarshal the field. CELExpression is the US-487
// alternative predicate shape — a CEL expression accessing the user.*
// and object.* bindings. Exactly one of Predicate / CELExpression must
// be populated.
type RowPolicy struct {
	RID           string          `json:"rid"`
	ObjectTypeRID string          `json:"objectTypeRid"`
	Predicate     json.RawMessage `json:"predicate,omitempty"`
	CELExpression string          `json:"celExpression,omitempty"`
	AppliesTo     AppliesTo       `json:"appliesTo"`
	Description   string          `json:"description,omitempty"`
	CreatedBy     string          `json:"createdBy,omitempty"`
	CreatedAt     time.Time       `json:"createdAt,omitempty"`
	UpdatedAt     time.Time       `json:"updatedAt,omitempty"`
}

// HasCEL reports whether this policy carries a US-487 CEL expression
// gate (vs the legacy WhereClause predicate). Used by the engine to
// route the policy into the CEL post-filter lane.
func (p *RowPolicy) HasCEL() bool {
	if p == nil {
		return false
	}
	return strings.TrimSpace(p.CELExpression) != ""
}

// Validate enforces required fields. Shape of the predicate is not checked
// here — the policy engine's Compile step runs it through
// where.ConvertToBleveQuery which rejects unknown / malformed clauses.
// For CEL-shaped policies, syntactic validation happens at Engine.Reload
// (via pkg/cel.Compile) so the error message can name the offending RID.
func (p *RowPolicy) Validate() error {
	if p == nil {
		return ErrObjectTypeRIDRequired
	}
	if strings.TrimSpace(p.ObjectTypeRID) == "" {
		return ErrObjectTypeRIDRequired
	}
	hasPred := len(p.Predicate) > 0 && string(p.Predicate) != "null"
	hasCEL := p.HasCEL()
	if !hasPred && !hasCEL {
		return ErrPredicateRequired
	}
	return nil
}

// RowPolicyUpdate is the PATCH shape for mutable fields. All pointer-typed
// so omit is distinguishable from explicit clear. CELExpression added in
// US-487 follows the same pointer convention — pass a pointer to "" to
// drop the CEL gate, or omit to leave the existing value untouched.
type RowPolicyUpdate struct {
	Predicate     *json.RawMessage `json:"predicate,omitempty"`
	CELExpression *string          `json:"celExpression,omitempty"`
	AppliesTo     *AppliesTo       `json:"appliesTo,omitempty"`
	Description   *string          `json:"description,omitempty"`
}
