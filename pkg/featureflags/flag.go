// Package featureflags implements a dynamic feature-flag system for
// gating functionality at runtime (US-276).
//
// A Flag has a globally-unique Name and a three-layer rollout model:
//
//	Enabled=false                    → always off, regardless of scope
//	Enabled=true, no scopes          → on for every caller
//	Enabled=true, Users=[...]        → on for listed user IDs
//	Enabled=true, Realms=[...]       → on for callers whose user.Attributes["realm"]
//	                                    is listed
//	Enabled=true, Users and Realms   → on if EITHER list matches (OR semantics)
//
// Flags persist in a small admin-managed table. Callers check flags via
// either Manager.HasFlag(ctx, name, user) directly or the ctx-backed
// HasFlag(ctx, name, user) helper that pulls the Manager out of request
// context.
package featureflags

import (
	"fmt"
	"regexp"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// Flag is one persisted feature-flag row.
type Flag struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	Realms      []string  `json:"realms,omitempty"`
	Users       []string  `json:"users,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// realmAttributeKey matches the convention used elsewhere in the
// codebase where per-tenant grouping is expressed as a string attribute
// on the authenticated user. We re-read it from User.Attributes rather
// than adding a new top-level User field so existing middleware that
// populates markings / attributes doesn't need a migration.
const realmAttributeKey = "realm"

// EnabledFor evaluates the three-layer rollout model against a user.
// See the package doc for semantics. Safe for a nil user — only
// globally-enabled flags without scopes match.
func (f Flag) EnabledFor(user *auth.User) bool {
	if !f.Enabled {
		return false
	}
	if len(f.Users) == 0 && len(f.Realms) == 0 {
		return true
	}
	if user == nil {
		return false
	}
	for _, id := range f.Users {
		if id == user.ID {
			return true
		}
	}
	if len(f.Realms) > 0 {
		realm := userRealm(user)
		if realm != "" {
			for _, r := range f.Realms {
				if r == realm {
					return true
				}
			}
		}
	}
	return false
}

// userRealm extracts the caller's realm attribute, if any. Returns ""
// when the user has no Attributes map or no "realm" key. Tolerant of
// the attribute being delivered as a native string or a generic any
// (JWT claim decode path).
func userRealm(user *auth.User) string {
	if user == nil || user.Attributes == nil {
		return ""
	}
	raw, ok := user.Attributes[realmAttributeKey]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

// flagNameRE is the canonical allowlist regex for flag names.
// Alphanumerics plus hyphen / underscore / dot, 1..128 chars. Same
// shape as property api_names elsewhere in the codebase.
var flagNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// ValidateFlagName returns an error when name is not an acceptable
// feature-flag identifier. Empty names, names with spaces, or names
// longer than 128 characters are rejected.
func ValidateFlagName(name string) error {
	if name == "" {
		return fmt.Errorf("feature flag name must not be empty")
	}
	if !flagNameRE.MatchString(name) {
		return fmt.Errorf("feature flag name %q is invalid: allowed characters are [A-Za-z0-9._-] and length must be 1..128", name)
	}
	return nil
}

// FlagUpdate is the partial-update payload for Store.UpdateFlag.
// Pointer fields let callers distinguish "omit=preserve" from
// "assign empty/false" — same pattern as oms.UpdateLinkTypeRequest.
type FlagUpdate struct {
	Description *string
	Enabled     *bool
	Realms      *[]string
	Users       *[]string
}
