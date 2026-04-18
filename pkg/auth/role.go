package auth

import (
	"errors"
	"regexp"
	"time"
)

// Role is a named collection of permissions. Built-in roles (viewer,
// editor, ontology-owner, admin, ingest-writer) are seeded by migration
// 000051 and mirror the static matrix in permissions.go. Custom roles
// registered via the admin API live alongside them; deletion of a
// built-in is blocked at the handler layer.
type Role struct {
	Name        string
	Description string
	Builtin     bool
	CreatedAt   time.Time
}

// MaxRoleNameLength bounds the role identifier. Roles travel in URLs
// (GET /api/admin/roles/{name}) and user_roles.role columns so the cap
// matches the 128-char ceiling used elsewhere.
const MaxRoleNameLength = 128

// roleNamePattern restricts names to alphanumerics, dots, hyphens, and
// underscores. Matches the existing built-ins (`ontology-owner`,
// `ingest-writer`) and rejects shell-metacharacters / spaces.
var roleNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._\-]*$`)

// ErrInvalidRoleName is returned by ValidateRoleName when the supplied
// string does not match the shape above.
var ErrInvalidRoleName = errors.New("invalid role name")

// ErrBuiltinRoleProtected is returned when an admin attempts to mutate or
// delete a built-in role. The static matrix in permissions.go owns the
// permission list for these roles; the admin API is restricted to custom
// roles.
var ErrBuiltinRoleProtected = errors.New("built-in role cannot be modified")

// ValidateRoleName enforces the on-wire role name shape: non-empty, up
// to MaxRoleNameLength bytes, starts with an alphanumeric, subsequent
// characters may include dot / hyphen / underscore.
func ValidateRoleName(name string) error {
	if name == "" {
		return ErrInvalidRoleName
	}
	if len(name) > MaxRoleNameLength {
		return ErrInvalidRoleName
	}
	if !roleNamePattern.MatchString(name) {
		return ErrInvalidRoleName
	}
	return nil
}
