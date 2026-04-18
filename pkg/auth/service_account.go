package auth

import (
	"errors"
	"regexp"
	"time"
)

// ServiceAccount is a non-interactive principal used by CI/CD pipelines and
// machine-to-machine integrations to authenticate against the Weave API.
//
// Each service account is owned by a human user (OwnerUserID) — that user's
// roles are the upper bound on what the service account can do. Scopes
// narrow the effective permission set further; an empty Scopes slice means
// "inherit everything the owner can do".
//
// Disabled service accounts survive in the DB as an audit trail; the unique
// partial index on name lets operators recreate a service account under the
// same name after disabling the previous one.
type ServiceAccount struct {
	ID           string
	Name         string
	Description  string
	OwnerUserID  string
	Scopes       []string
	ExpiresAt    *time.Time
	DisabledAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// IsDisabled reports whether the service account has been administratively
// disabled via DELETE /api/admin/service-accounts/{id}.
func (s *ServiceAccount) IsDisabled() bool { return s != nil && s.DisabledAt != nil }

// IsExpired reports whether the service account's optional absolute expiry
// has passed at the supplied "now". A nil ExpiresAt means no expiry.
func (s *ServiceAccount) IsExpired(now time.Time) bool {
	if s == nil || s.ExpiresAt == nil {
		return false
	}
	return now.After(*s.ExpiresAt)
}

// IsActive reports whether the service account is eligible to authenticate
// requests at "now": not disabled AND not expired.
func (s *ServiceAccount) IsActive(now time.Time) bool {
	if s == nil {
		return false
	}
	return !s.IsDisabled() && !s.IsExpired(now)
}

// MaxServiceAccountNameLength bounds the human-friendly name so a runaway
// client cannot exhaust the column. 128 matches the existing limits used on
// ObjectType / LinkType names elsewhere in the codebase.
const MaxServiceAccountNameLength = 128

// serviceAccountNamePattern restricts names to characters that survive being
// embedded in URLs and shell invocations without escaping. The PG column is
// TEXT so the DB itself accepts anything — validation happens at the HTTP
// boundary so a malformed payload fails fast with a 400 rather than being
// persisted and causing mystery bugs downstream.
var serviceAccountNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-]*$`)

// ErrInvalidServiceAccountName is returned by ValidateServiceAccountName
// when the supplied string does not match the shape above.
var ErrInvalidServiceAccountName = errors.New("invalid service account name")

// ValidateServiceAccountName enforces the on-wire name shape. The rules:
// non-empty, up to MaxServiceAccountNameLength bytes, starts with an
// alphanumeric, subsequent characters may also include dot / hyphen /
// underscore. No spaces, slashes, or other shell-metacharacters.
func ValidateServiceAccountName(name string) error {
	if name == "" {
		return ErrInvalidServiceAccountName
	}
	if len(name) > MaxServiceAccountNameLength {
		return ErrInvalidServiceAccountName
	}
	if !serviceAccountNamePattern.MatchString(name) {
		return ErrInvalidServiceAccountName
	}
	return nil
}
