// Package actiontemplates implements per-user named parameter
// templates for the Action Console (US-320). Templates capture a
// {parameterId: value} map keyed by ontology + actionType apiName, so
// the user can recall previously-used parameter sets without retyping
// them.
//
// Each row carries a `shared` flag: private templates are visible
// only to their creator; shared templates are visible to anyone who
// can read the parent action type. Update/Delete are always
// owner-only — `shared` widens read access, never write access.
package actiontemplates

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
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
	Shared     bool            `json:"shared"`
	Parameters json.RawMessage `json:"parameters"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// Update is the partial-update DTO. nil pointer fields preserve the
// existing value; non-nil overwrites. Identity columns
// (id/ontology/actionType/createdBy) are not mutable — moving a
// template across action types creates a new row.
type Update struct {
	Name       *string          `json:"name,omitempty"`
	Parameters *json.RawMessage `json:"parameters,omitempty"`
	Shared     *bool            `json:"shared,omitempty"`
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
