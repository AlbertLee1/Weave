// Package savedsearches implements per-user named search persistence
// for the Browser page (US-311). One row captures the search text +
// structured filters + facet selections + sort that the user wants to
// recall later.
package savedsearches

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// SavedSearch is the wire + DB shape for a persisted saved search.
//
// Definition is intentionally an opaque JSONB envelope keyed by the
// front-end. Backend treats it as round-trip storage so the SPA can
// evolve the payload (filters, facets, sort, search text) without a
// schema change. The minimum useful shape is documented in the
// SavedSearchDefinition type.
type SavedSearch struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Ontology   string          `json:"ontology"`
	ObjectType string          `json:"objectType"`
	CreatedBy  string          `json:"createdBy"`
	Definition json.RawMessage `json:"definition"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// Update is the partial-update DTO. nil pointer fields preserve the
// existing value; non-nil overwrites. Identity columns
// (id/ontology/objectType/createdBy) are not mutable.
type Update struct {
	Name       *string          `json:"name,omitempty"`
	Definition *json.RawMessage `json:"definition,omitempty"`
}

// MaxNameLength bounds the name on both create and update so the
// CHECK constraint and the Go-side validation agree.
const MaxNameLength = 128

// ValidateName ensures a saved-search name is non-empty after trim and
// no longer than MaxNameLength. Centralised so handlers, store impls,
// and tests share the same rule.
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

// ValidateScope enforces non-empty ontology / objectType identifiers so
// a saved row can always be List-scoped from the Browser page.
func ValidateScope(ontology, objectType string) error {
	if strings.TrimSpace(ontology) == "" {
		return errors.New("ontology must not be empty")
	}
	if strings.TrimSpace(objectType) == "" {
		return errors.New("objectType must not be empty")
	}
	return nil
}
