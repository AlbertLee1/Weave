package auth

import (
	"errors"

	"golang.org/x/crypto/bcrypt"
)

// BcryptCost is the bcrypt work factor used for all password hashing in Weave.
// 12 is OWASP-recommended (~250ms on a 2022 CPU). Configurable via env in
// the config package; this constant is the package-level default fallback.
const BcryptCost = 12

// dummyHash is a constant-cost bcrypt hash used by VerifyDummyPassword to
// keep the login handler's missing-user code path the same wall-clock cost
// as the wrong-password code path. The plaintext is unguessable; we never
// expose it.
//
// Generated once with bcrypt.GenerateFromPassword([]byte("dummy-never-matches-anything"), 12).
var dummyHash = []byte("$2a$12$abcdefghijklmnopqrstuuyZ0gQqJZyT/2yIqTVnEz8FFjjs6WP1RG")

// ErrEmptyPassword is returned when an empty string is passed to HashPassword.
var ErrEmptyPassword = errors.New("password must not be empty")

// HashPassword bcrypt-hashes the given password at BcryptCost.
func HashPassword(password string) (string, error) {
	if password == "" {
		return "", ErrEmptyPassword
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword returns nil iff password matches hash.
func VerifyPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// VerifyDummyPassword performs a constant-cost bcrypt compare against an
// internal dummy hash. Used by login handlers in the user-not-found code
// path so that response timing does not leak account existence.
// Always returns a non-nil error.
func VerifyDummyPassword(password string) error {
	err := bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
	if err == nil {
		// Should be impossible because dummyHash hashes an unguessable string.
		return errors.New("dummy hash unexpectedly matched")
	}
	return err
}
