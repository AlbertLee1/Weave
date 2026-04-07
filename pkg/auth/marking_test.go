package auth_test

import (
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// TestMarking_Equality verifies that two Marking values with the same fields
// compare equal under the standard struct equality. This guards against
// accidentally adding non-comparable fields (slices, maps) to the struct;
// markings need to be cheap value types so they can sit in caches and
// keysets without bookkeeping.
func TestMarking_Equality(t *testing.T) {
	now := time.Date(2026, 4, 6, 0, 0, 0, 0, time.UTC)
	a := auth.Marking{
		Name:        "PII",
		DisplayName: "PII",
		Description: "Personally identifiable information",
		Color:       "#ef4444",
		CreatedAt:   now,
	}
	b := auth.Marking{
		Name:        "PII",
		DisplayName: "PII",
		Description: "Personally identifiable information",
		Color:       "#ef4444",
		CreatedAt:   now,
	}
	if a != b {
		t.Errorf("expected identical markings to be equal, got a=%+v b=%+v", a, b)
	}

	c := auth.Marking{
		Name:        "PUBLIC",
		DisplayName: "Public",
		Color:       "#10b981",
		CreatedAt:   now,
	}
	if a == c {
		t.Errorf("expected differing markings to be unequal, got a=%+v c=%+v", a, c)
	}
}

// TestMarkingGrant_Struct verifies that the MarkingGrant struct carries
// every field the auth/middleware path needs to render a meaningful audit
// row, and that zero values are usable (no required pointer types).
func TestMarkingGrant_Struct(t *testing.T) {
	when := time.Date(2026, 4, 6, 12, 0, 0, 0, time.UTC)
	g := auth.MarkingGrant{
		UserID:      "user:alice@example.com",
		MarkingName: "CONFIDENTIAL",
		GrantedAt:   when,
		GrantedBy:   "user:admin",
	}

	if g.UserID != "user:alice@example.com" {
		t.Errorf("expected UserID to be set, got %q", g.UserID)
	}
	if g.MarkingName != "CONFIDENTIAL" {
		t.Errorf("expected MarkingName to be set, got %q", g.MarkingName)
	}
	if !g.GrantedAt.Equal(when) {
		t.Errorf("expected GrantedAt to be set, got %v", g.GrantedAt)
	}
	if g.GrantedBy != "user:admin" {
		t.Errorf("expected GrantedBy to be set, got %q", g.GrantedBy)
	}

	// Zero value sanity: an empty MarkingGrant should be usable as a
	// placeholder without panicking on any field access.
	var zero auth.MarkingGrant
	if zero.UserID != "" || zero.MarkingName != "" || zero.GrantedBy != "" {
		t.Errorf("expected zero MarkingGrant to have empty strings, got %+v", zero)
	}
	if !zero.GrantedAt.IsZero() {
		t.Errorf("expected zero MarkingGrant to have zero time, got %v", zero.GrantedAt)
	}
}
