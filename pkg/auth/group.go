package auth

import (
	"errors"
	"regexp"
	"time"
)

// Group is a named collection of users. Membership lives in user_groups.
// Groups are the first-class handle for "bulk-grant a role" workflows and
// appear in the admin CRUD UI. Names are globally unique.
type Group struct {
	ID          string
	Name        string
	Description string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// MaxGroupNameLength bounds the human-friendly name. Matches the 128-char
// cap used on ObjectType / LinkType / ServiceAccount names elsewhere.
const MaxGroupNameLength = 128

// groupNamePattern accepts alphanumeric starts followed by alphanumerics,
// dots, hyphens, or underscores. Same shape as service account names so
// both can share URL path segments without escaping.
var groupNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-]*$`)

// ErrInvalidGroupName is returned by ValidateGroupName when the supplied
// string does not match the shape above.
var ErrInvalidGroupName = errors.New("invalid group name")

// ValidateGroupName enforces the on-wire name shape: non-empty, up to
// MaxGroupNameLength bytes, starts with an alphanumeric, subsequent
// characters may include dot / hyphen / underscore.
func ValidateGroupName(name string) error {
	if name == "" {
		return ErrInvalidGroupName
	}
	if len(name) > MaxGroupNameLength {
		return ErrInvalidGroupName
	}
	if !groupNamePattern.MatchString(name) {
		return ErrInvalidGroupName
	}
	return nil
}
