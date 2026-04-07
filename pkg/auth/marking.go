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
type MarkingGrant struct {
	UserID      string
	MarkingName string
	GrantedAt   time.Time
	GrantedBy   string
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
