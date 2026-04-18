package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// DefaultMFAIssuer is the issuer label embedded in the otpauth:// URL.
// Authenticator apps render this string as the account scope, so keep it
// stable — operators should NOT change it after users have enrolled or
// existing TOTP secrets will appear as duplicates in the user's app.
const DefaultMFAIssuer = "Weave"

// MFASecretStore is the narrow persistence surface used by MFAHandler.
// Kept off UserRepository so the ~15 in-memory user-repo mocks scattered
// through the test tree don't need to grow stubs (same pattern as
// UserRoleRevoker / ServiceAccountRepository / MediaAssetStore).
type MFASecretStore interface {
	// SetMFASecret writes the base32 TOTP shared secret. Does not
	// activate enforcement; call SetMFAEnabled(true) after the user
	// proves possession of the secret with a valid code.
	SetMFASecret(ctx context.Context, userID, secret string) error
	// SetMFAEnabled flips the enforcement flag. False means subsequent
	// logins skip the second-factor challenge.
	SetMFAEnabled(ctx context.Context, userID string, enabled bool) error
	// ClearMFA wipes both the secret and the enabled flag in one round
	// trip. Used by /api/auth/mfa/disable.
	ClearMFA(ctx context.Context, userID string) error
}

// SetMFASecret writes the base32 TOTP shared secret for a user.
func (r *PGUserRepository) SetMFASecret(ctx context.Context, userID, secret string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET mfa_secret = NULLIF($1, ''), updated_at = now() WHERE id = $2`,
		secret, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// SetMFAEnabled flips the mfa_enabled flag for a user.
func (r *PGUserRepository) SetMFAEnabled(ctx context.Context, userID string, enabled bool) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET mfa_enabled = $1, updated_at = now() WHERE id = $2`,
		enabled, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ClearMFA resets both the secret and the enforcement flag.
func (r *PGUserRepository) ClearMFA(ctx context.Context, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET mfa_secret = NULL, mfa_enabled = FALSE, updated_at = now() WHERE id = $1`,
		userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// GenerateTOTPSecret creates a fresh TOTP key bound to the supplied account
// (typically the user's email). Returns the otp.Key so callers can render
// both the base32 secret and the otpauth:// URL.
func GenerateTOTPSecret(issuer, account string) (*otp.Key, error) {
	if account == "" {
		return nil, errors.New("account required")
	}
	if issuer == "" {
		issuer = DefaultMFAIssuer
	}
	return totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
}

// ValidateTOTPCode returns nil iff the supplied 6-digit code matches the
// secret for the supplied moment in time. A ±1-step skew (30s before / after)
// is accepted to tolerate small clock drift between the user's device and
// the server. Empty secret or empty code is rejected.
func ValidateTOTPCode(secret, code string, now time.Time) error {
	if secret == "" {
		return errors.New("mfa secret missing")
	}
	if code == "" {
		return errors.New("mfa code required")
	}
	ok, err := totp.ValidateCustom(code, secret, now, totp.ValidateOpts{
		Period:    30,
		Skew:      1,
		Digits:    otp.DigitsSix,
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return fmt.Errorf("validate totp: %w", err)
	}
	if !ok {
		return ErrInvalidMFACode
	}
	return nil
}

// ErrInvalidMFACode is returned by ValidateTOTPCode when the code does not
// match the secret (after skew). Surfaces as 401 InvalidMFACode at the HTTP
// boundary.
var ErrInvalidMFACode = errors.New("invalid mfa code")
