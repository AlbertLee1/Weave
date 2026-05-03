// Package quiver implements per-owner persistence of Quiver workbench
// dashboards (US-403). One row captures the full workbench config —
// series spec list, color choices, optional saved selection range — as
// an opaque JSONB envelope so the SPA can evolve the wire shape without
// a schema change.
//
// Reads on a known RID are open to any authenticated caller (the
// read-only `/quiver/{rid}/view` route is the share-link semantic);
// mutating routes (save / delete) and List remain owner-scoped.
package quiver

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Dashboard is the wire + DB shape for a persisted Quiver dashboard.
//
// Config is intentionally an opaque JSONB envelope keyed by the SPA.
// The minimum useful shape carries a `series[]` array of {objectType,
// primaryKey, property, label, color}; new keys (transform chain,
// y-axis bounds, default selection) can be added without a backend
// change.
type Dashboard struct {
	RID       string          `json:"rid"`
	Name      string          `json:"name"`
	Owner     string          `json:"owner"`
	Config    json.RawMessage `json:"config"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// Update is the partial-update DTO. nil pointer fields preserve the
// existing value; non-nil overwrites. Identity columns (rid/owner) are
// not mutable.
type Update struct {
	Name   *string          `json:"name,omitempty"`
	Config *json.RawMessage `json:"config,omitempty"`
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
