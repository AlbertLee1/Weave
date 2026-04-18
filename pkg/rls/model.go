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
// view can json.Unmarshal the field.
type RowPolicy struct {
	RID           string          `json:"rid"`
	ObjectTypeRID string          `json:"objectTypeRid"`
	Predicate     json.RawMessage `json:"predicate"`
	AppliesTo     AppliesTo       `json:"appliesTo"`
	Description   string          `json:"description,omitempty"`
	CreatedBy     string          `json:"createdBy,omitempty"`
	CreatedAt     time.Time       `json:"createdAt,omitempty"`
	UpdatedAt     time.Time       `json:"updatedAt,omitempty"`
}

// Validate enforces required fields. Shape of the predicate is not checked
// here — the policy engine's Compile step runs it through
// where.ConvertToBleveQuery which rejects unknown / malformed clauses.
func (p *RowPolicy) Validate() error {
	if p == nil {
		return ErrObjectTypeRIDRequired
	}
	if strings.TrimSpace(p.ObjectTypeRID) == "" {
		return ErrObjectTypeRIDRequired
	}
	if len(p.Predicate) == 0 || string(p.Predicate) == "null" {
		return ErrPredicateRequired
	}
	return nil
}

// RowPolicyUpdate is the PATCH shape for mutable fields. All pointer-typed
// so omit is distinguishable from explicit clear.
type RowPolicyUpdate struct {
	Predicate   *json.RawMessage `json:"predicate,omitempty"`
	AppliesTo   *AppliesTo       `json:"appliesTo,omitempty"`
	Description *string          `json:"description,omitempty"`
}
