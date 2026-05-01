// Package userprefs implements the per-user preference center (US-350).
// One row in the `user_preferences` table captures the four sections
// surfaced by the /settings page: theme, language, notifications, and
// hotkey overrides. Each section persists as a small JSONB envelope so
// the SPA can evolve the payload without a schema change.
package userprefs

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ValidThemes is the closed enum recognised by the theme column. Empty
// string means "no preference" — the SPA falls back to its localStorage
// / OS default. Mirrors the CHECK constraint added in migration 000081.
var ValidThemes = []string{"", "dark", "light", "system"}

// MaxLanguageLength bounds the language column so the Go-side validator
// agrees with the migration CHECK. 32 chars is generous for any tag
// shape we'll see (e.g. "zh-Hans-CN").
const MaxLanguageLength = 32

// Preferences is the wire + DB shape for one user's persisted settings.
//
// Notifications and Hotkeys are intentionally opaque JSONB envelopes
// keyed by the front-end. The backend round-trips the bytes so the SPA
// can evolve sub-payloads (channel toggles, hotkey overrides, etc.)
// without a schema change. Empty objects ("{}") are the canonical
// "no overrides" default.
type Preferences struct {
	UserID        string          `json:"userId"`
	Theme         string          `json:"theme"`
	Language      string          `json:"language"`
	Notifications json.RawMessage `json:"notifications"`
	Hotkeys       json.RawMessage `json:"hotkeys"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

// Update is the partial-update DTO. Nil pointer fields preserve the
// existing value; non-nil overwrites. UserID/CreatedAt are not mutable.
type Update struct {
	Theme         *string          `json:"theme,omitempty"`
	Language      *string          `json:"language,omitempty"`
	Notifications *json.RawMessage `json:"notifications,omitempty"`
	Hotkeys       *json.RawMessage `json:"hotkeys,omitempty"`
}

// ValidateTheme accepts the closed enum only. The empty string is a
// valid "no preference" sentinel.
func ValidateTheme(theme string) error {
	for _, v := range ValidThemes {
		if theme == v {
			return nil
		}
	}
	return errors.New("invalid theme: must be one of '', 'dark', 'light', 'system'")
}

// ValidateLanguage rejects anything longer than MaxLanguageLength so the
// Go-side gate matches the DB CHECK. The empty string is allowed.
func ValidateLanguage(language string) error {
	if len(language) > MaxLanguageLength {
		return errors.New("language exceeds maximum length")
	}
	return nil
}

// NormaliseTheme trims whitespace before validation so "  dark  " and
// "dark" are equivalent on the wire.
func NormaliseTheme(theme string) string {
	return strings.TrimSpace(theme)
}

// NormaliseLanguage trims whitespace before validation.
func NormaliseLanguage(language string) string {
	return strings.TrimSpace(language)
}

// DefaultPayload is the canonical "empty JSONB envelope" for the
// notifications and hotkeys columns. Keep both the read and write
// helpers in sync — pgx encodes a nil json.RawMessage as the string
// "null", which the column happily accepts but breaks the
// "absent ⇒ {}" round-trip. Mirrors pkg/oms.normaliseSignatureForWrite.
var DefaultPayload = json.RawMessage("{}")
