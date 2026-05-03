package apps

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// App is the wire + DB shape for a versioned Workshop-lite App (US-391).
//
// LayoutJSON is intentionally an opaque JSONB envelope keyed by the
// front-end. The minimum shape carries a root row/col tree (see
// ValidateLayout) but new components, props, and metadata can be added
// without a backend schema change.
//
// Version is the monotonically-increasing version counter on the live
// `apps` row — every update bumps it AND inserts a snapshot into the
// `app_versions` history table. Version starts at 1 on Create.
//
// PublishedVersion / PublishedAt / PublishedBy together encode the
// US-396 publish state. An App is "published" iff PublishedVersion is
// non-nil; the three fields are stamped + cleared atomically.
type App struct {
	RID              string          `json:"rid"`
	Name             string          `json:"name"`
	OwnerID          string          `json:"ownerId"`
	LayoutJSON       json.RawMessage `json:"layoutJson"`
	Version          int             `json:"version"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	PublishedVersion *int            `json:"publishedVersion,omitempty"`
	PublishedAt      *time.Time      `json:"publishedAt,omitempty"`
	PublishedBy      *string         `json:"publishedBy,omitempty"`
}

// AppVersion is one historical snapshot of an App. The `app_versions`
// table grows on every successful Update; the most-recent row's
// LayoutJSON / Version match the live `apps` row's columns.
type AppVersion struct {
	AppRID     string          `json:"appRid"`
	Version    int             `json:"version"`
	Name       string          `json:"name"`
	LayoutJSON json.RawMessage `json:"layoutJson"`
	CreatedAt  time.Time       `json:"createdAt"`
	CreatedBy  string          `json:"createdBy"`
}

// Update is the partial-update DTO. nil pointer fields preserve the
// existing column; non-nil overwrites. Identity columns (rid/ownerId)
// are not mutable. Bumping Version + inserting an AppVersion row is the
// store implementation's responsibility — callers do not stamp Version
// here.
type Update struct {
	Name       *string          `json:"name,omitempty"`
	LayoutJSON *json.RawMessage `json:"layoutJson,omitempty"`
}

// MaxNameLength bounds the App name on both create and update so the
// CHECK constraint and the Go-side validation agree.
const MaxNameLength = 128

// ValidateName ensures an App name is non-empty after trim and no
// longer than MaxNameLength. Centralised so handlers, store impls, and
// tests share the same rule.
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
