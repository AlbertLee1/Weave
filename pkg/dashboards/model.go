// Package dashboards implements per-user dashboard persistence with
// optional public sharing (US-329). One row captures the dashboard's
// widget layout + display metadata as an opaque JSONB envelope so the
// SPA can evolve the on-the-wire shape without a schema change.
package dashboards

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Dashboard is the wire + DB shape for a persisted dashboard.
//
// Definition is intentionally an opaque JSONB envelope keyed by the
// front-end. The minimum useful shape carries `widgets[]`; new keys
// (theme, refresh interval, parameters) can be added without a backend
// change.
//
// IsPublic toggles share-link semantics: when true, any authenticated
// caller can GET this dashboard by id; when false, the row is private
// to its creator.
type Dashboard struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	CreatedBy  string          `json:"createdBy"`
	IsPublic   bool            `json:"isPublic"`
	Definition json.RawMessage `json:"definition"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  time.Time       `json:"updatedAt"`
}

// Update is the partial-update DTO. nil pointer fields preserve the
// existing value; non-nil overwrites. Identity columns (id/createdBy)
// are not mutable.
type Update struct {
	Name       *string          `json:"name,omitempty"`
	Definition *json.RawMessage `json:"definition,omitempty"`
	IsPublic   *bool            `json:"isPublic,omitempty"`
}

// MaxNameLength bounds the name on both create and update so the CHECK
// constraint and the Go-side validation agree.
const MaxNameLength = 128

// ValidateName ensures a dashboard name is non-empty after trim and
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
