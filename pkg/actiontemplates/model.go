// Package actiontemplates implements per-user named parameter
// templates for the Action Console (US-320, extended in US-427).
// Templates capture a {parameterId: value} map keyed by ontology +
// actionType apiName, so the user can recall previously-used
// parameter sets without retyping them.
//
// Each row carries a 3-state Scope (PRIVATE | TEAM | PUBLIC):
//
//   - PRIVATE: only the creator can read.
//   - TEAM:    creator + any user who shares at least one
//              auth.Group with the creator can read.
//   - PUBLIC:  any authenticated user can read.
//
// Update/Delete are always owner-only — Scope widens read access,
// never write access. The legacy boolean Shared (US-320) is preserved
// in the wire shape and derived from Scope: Shared == (Scope !=
// "PRIVATE"). On write, callers may supply either dimension; if both
// are absent the row is private.
package actiontemplates

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Scope values. Stored verbatim in the action_parameter_templates.scope
// column under a CHECK constraint enforced by migration 000101.
const (
	ScopePrivate = "PRIVATE"
	ScopeTeam    = "TEAM"
	ScopePublic  = "PUBLIC"
)

// Template is the wire + DB shape for a persisted parameter template.
//
// Parameters is intentionally an opaque JSONB envelope keyed by
// parameter id; the front end owns the schema (it mirrors the
// ActionType.parameters definition) and the backend round-trips it
// untouched.
type Template struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Ontology   string          `json:"ontology"`
	ActionType string          `json:"actionType"`
	CreatedBy  string          `json:"createdBy"`
	Scope      string          `json:"scope"`
	Shared     bool            `json:"shared"`
	Parameters json.RawMessage `json:"parameters"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// Update is the partial-update DTO. nil pointer fields preserve the
// existing value; non-nil overwrites. Identity columns
// (id/ontology/actionType/createdBy) are not mutable — moving a
// template across action types creates a new row.
//
// Both Scope and Shared are accepted on the wire for backwards
// compatibility with US-320 clients. When Scope is provided it wins;
// otherwise Shared is mapped (true→PUBLIC, false→PRIVATE).
type Update struct {
	Name       *string          `json:"name,omitempty"`
	Parameters *json.RawMessage `json:"parameters,omitempty"`
	Shared     *bool            `json:"shared,omitempty"`
	Scope      *string          `json:"scope,omitempty"`
}

// MaxNameLength bounds the name on both create and update so the
// CHECK constraint and the Go-side validation agree.
const MaxNameLength = 128

// ValidateName ensures a template name is non-empty after trim and
// no longer than MaxNameLength.
func ValidateName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return errors.New("name must not be empty")
	}
	if len(trimmed) > MaxNameLength {
		return errors.New("name exceeds maximum length")
	}
	return nil
}

// ValidateScope enforces non-empty ontology / actionType identifiers
// so a saved row can always be List-scoped from the Action Console.
func ValidateScope(ontology, actionType string) error {
	if strings.TrimSpace(ontology) == "" {
		return errors.New("ontology must not be empty")
	}
	if strings.TrimSpace(actionType) == "" {
		return errors.New("actionType must not be empty")
	}
	return nil
}

// NormaliseScope maps wire input to the canonical uppercase Scope
// constant. Unknown values surface as an error so an SDK client never
// silently downgrades to PRIVATE.
func NormaliseScope(raw string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(raw)) {
	case "", ScopePrivate:
		return ScopePrivate, nil
	case ScopeTeam:
		return ScopeTeam, nil
	case ScopePublic:
		return ScopePublic, nil
	default:
		return "", errors.New("scope must be PRIVATE, TEAM, or PUBLIC")
	}
}

// ScopeFromShared maps the legacy boolean to the canonical Scope:
// shared=TRUE → PUBLIC, shared=FALSE → PRIVATE.
func ScopeFromShared(shared bool) string {
	if shared {
		return ScopePublic
	}
	return ScopePrivate
}

// SharedFromScope is the inverse projection used to keep the legacy
// boolean visible in the wire response.
func SharedFromScope(scope string) bool {
	return scope != ScopePrivate
}
