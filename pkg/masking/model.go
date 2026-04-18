// Package masking implements US-257 column-level masking: per-(ObjectType,
// property) value transforms applied at response-serialisation time to hide
// sensitive fields from callers outside an allow list.
//
// A ColumnMask carries three things: the ObjectType it applies to, the
// property apiName whose value should be rewritten, and an AppliesTo scope
// (roles/groups/users). The AppliesTo set identifies the callers that are
// ALLOWED to see the clear value; every other authenticated caller receives
// the masked value. Admins (PermUserManage) bypass all masks.
package masking

import (
	"errors"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// MaskRule names a value-rewriting transform. Known rules live in this file;
// adding a new rule requires: (1) a MaskRule<Name> constant, (2) inclusion in
// IsKnownRule, and (3) a branch in ApplyMaskRule.
type MaskRule string

const (
	MaskRuleHash    MaskRule = "hash"
	MaskRuleRedact  MaskRule = "redact"
	MaskRulePartial MaskRule = "partial"
)

// IsKnownRule reports whether r is a recognised MaskRule. Used by the
// validator and handler-side input checks so unknown strings are rejected
// at the API boundary rather than silently passing values through.
func IsKnownRule(r MaskRule) bool {
	switch r {
	case MaskRuleHash, MaskRuleRedact, MaskRulePartial:
		return true
	default:
		return false
	}
}

// Sentinel errors surfaced by Validate and by Store implementations.
var (
	ErrObjectTypeRIDRequired = errors.New("objectTypeRID is required")
	ErrPropertyRequired      = errors.New("propertyApiName is required")
	ErrMaskRuleRequired      = errors.New("maskRule is required")
	ErrUnknownMaskRule       = errors.New("unknown maskRule")
	ErrNotFound              = errors.New("column mask not found")
)

// AppliesTo carries the identity set ALLOWED to see the clear (unmasked)
// value. Callers matching any dimension (Roles / Groups / Users) see the
// property untouched; every other authenticated caller receives the masked
// value. Empty AppliesTo means "no one is on the allow list" → mask applies
// to everyone (admins still bypass via PermUserManage).
type AppliesTo struct {
	Roles  []string `json:"roles,omitempty"`
	Groups []string `json:"groups,omitempty"`
	Users  []string `json:"users,omitempty"`
}

// IsApplicable reports whether user is inside the allow list.
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

// ColumnMask is one row of the column_masks table.
type ColumnMask struct {
	RID             string    `json:"rid"`
	ObjectTypeRID   string    `json:"objectTypeRid"`
	PropertyAPIName string    `json:"propertyApiName"`
	MaskRule        MaskRule  `json:"maskRule"`
	AppliesTo       AppliesTo `json:"appliesTo"`
	Description     string    `json:"description,omitempty"`
	CreatedBy       string    `json:"createdBy,omitempty"`
	CreatedAt       time.Time `json:"createdAt,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt,omitempty"`
}

// Validate enforces required fields and rule-name canonicalisation.
func (m *ColumnMask) Validate() error {
	if m == nil {
		return ErrObjectTypeRIDRequired
	}
	if strings.TrimSpace(m.ObjectTypeRID) == "" {
		return ErrObjectTypeRIDRequired
	}
	if strings.TrimSpace(m.PropertyAPIName) == "" {
		return ErrPropertyRequired
	}
	if strings.TrimSpace(string(m.MaskRule)) == "" {
		return ErrMaskRuleRequired
	}
	if !IsKnownRule(m.MaskRule) {
		return ErrUnknownMaskRule
	}
	return nil
}

// ColumnMaskUpdate is the PATCH shape for mutable fields. All pointer-typed
// so "omit" (preserve) is distinguishable from "explicit value".
type ColumnMaskUpdate struct {
	MaskRule    *MaskRule  `json:"maskRule,omitempty"`
	AppliesTo   *AppliesTo `json:"appliesTo,omitempty"`
	Description *string    `json:"description,omitempty"`
}
