package auth

import "time"

// Marking is a classification label that can be attached to objects to
// require mandatory access control. Unlike SecurityPolicies (ABAC, where
// effect rules can allow or deny based on properties), markings are a
// strict subset check: a user must hold a grant for *every* marking on an
// object before they may see it. There is no condition that overrides a
// missing grant; only an explicit row in user_markings unlocks a marking.
//
// Markings are seeded by the migration with five well-known names:
// PUBLIC, INTERNAL, CONFIDENTIAL, PII, SECRET. Operators may add custom
// markings via the markings table.
type Marking struct {
	Name        string
	DisplayName string
	Description string
	Color       string
	CreatedAt   time.Time
}

// MarkingGrant is the audit-friendly representation of a single
// (user, marking) row in the user_markings join table. The MarkingFilter
// loads only the marking names for the request hot path, but admin and
// audit handlers want the full grant record so they can render who
// granted what and when.
//
// ExpiresAt is the optional auto-revocation instant. A nil pointer means
// the grant is permanent (pre-US-260 default). A non-nil pointer is the
// exact timestamp at which the grant stops surfacing in GetUserMarkings;
// the filter is applied in SQL via `WHERE (expires_at IS NULL OR
// expires_at > NOW())` so expired grants never reach the OSS hot path.
type MarkingGrant struct {
	UserID      string
	MarkingName string
	GrantedAt   time.Time
	GrantedBy   string
	ExpiresAt   *time.Time
}

// IsExpired reports whether the grant's expires_at is in the past at now.
// A permanent grant (ExpiresAt == nil) always returns false.
func (g MarkingGrant) IsExpired(now time.Time) bool {
	if g.ExpiresAt == nil {
		return false
	}
	return !now.Before(*g.ExpiresAt)
}

// EvaluateMarkings implements Foundry-style mandatory access control for
// object markings. It returns true iff the caller holds every marking the
// object carries (set containment, AND semantics).
//
// Behaviour:
//   - objectMarkings empty/nil → true (unmarked objects are public).
//   - userMarkings is missing any required marking → false (fail-closed).
//   - Comparison is case-sensitive and duplicate-tolerant on both sides.
//
// This is the single source of truth for marking evaluation; row-level
// policy integration (US-051) and JWT claim injection (US-053) both feed
// into this function rather than re-implementing the subset check.
func EvaluateMarkings(userMarkings, objectMarkings []string) bool {
	if len(objectMarkings) == 0 {
		return true
	}
	held := make(map[string]struct{}, len(userMarkings))
	for _, m := range userMarkings {
		held[m] = struct{}{}
	}
	for _, req := range objectMarkings {
		if _, ok := held[req]; !ok {
			return false
		}
	}
	return true
}

// MarkingsField is the reserved keyword field name on every indexed
// document that stores the row's marking labels. Writers (action
// executor, funnel consumer, importers) populate this field with a
// []string when they create or update an object, and the OSS read path
// uses it to drop rows the requesting user is not cleared for.
//
// Documents that omit the field entirely are treated as PUBLIC and
// visible to everyone, which keeps existing un-marked datasets working
// without a backfill.
const MarkingsField = "__markings"
