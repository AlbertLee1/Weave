package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"strings"
	"time"
)

// API key format and constants.
//
// Raw key format: "wvk_<prefix>_<random>"
//   - "wvk_" is the literal Weave-key marker that lets the auth middleware
//     route a bearer token to the API-key code path before attempting JWT
//     verification.
//   - <prefix> is APIKeyPrefixLen base32 characters derived from random bytes.
//     It is stored in the DB and used as the lookup index so we can find a key
//     row in O(1) without scanning every row.
//   - <random> is APIKeyRandomLen base32 characters (256 bits of entropy) and
//     is NEVER persisted; only its SHA-256 hash is stored. The constant-time
//     comparison happens on the SHA-256 digest.
const (
	APIKeyMarker    = "wvk_"
	APIKeyPrefixLen = 8  // base32 characters of the prefix segment
	APIKeyRandomLen = 52 // base32 characters of the random segment (32 bytes -> 52 chars unpadded)
)

// b32 is a non-padded uppercase base32 alphabet (RFC 4648). We strip padding
// because tokens are easier to copy/paste without "=" characters.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// ErrInvalidAPIKeyFormat is returned by ParseAPIKey when the supplied string
// does not match the expected wvk_<prefix>_<random> shape.
var ErrInvalidAPIKeyFormat = errors.New("invalid api key format")

// DefaultAPIKeyRotationGrace is the default predecessor-successor overlap
// window applied by APIKeyHandler.Rotate when the caller does not pass an
// explicit graceDays. It matches the PRD ("Grace period：双 key 并存 7 天").
const DefaultAPIKeyRotationGrace = 7 * 24 * time.Hour

// DefaultAPIKeyRotationWarning is the look-ahead window for imminent-rotation
// warnings: ListPendingRotations surfaces every key whose rotates_at falls
// within now..now+window so operators can notify the owning service ahead of
// the cut-off. PRD: "到期前 7 天发送告警事件".
const DefaultAPIKeyRotationWarning = 7 * 24 * time.Hour

// APIKeyRecord is the persistent representation of an API key row.
//
// The raw key is NEVER stored. Only its SHA-256 hash and the lookup-only
// prefix live in the database. RawKey is populated only on the response from
// CreateAPIKey so that the operator can copy the secret once at creation time.
//
// Rotation fields:
//   - RotatesAt: when set, the row is scheduled for automatic rotation; the
//     middleware treats the key as expired once now >= *RotatesAt, so the
//     window [now, *RotatesAt) is the grace period during which both the
//     predecessor and its successor validate.
//   - SuccessorID: populated on the predecessor row once Rotate mints the
//     replacement. A non-nil successor means the row is already in rotation
//     and another Rotate call must fail.
type APIKeyRecord struct {
	ID          string
	KeyHash     []byte
	KeyPrefix   string
	UserID      string
	Name        string
	Scopes      []string
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
	LastUsedAt  *time.Time
	RotatesAt   *time.Time
	SuccessorID *string
}

// IsRevoked reports whether the key has been administratively revoked.
func (k *APIKeyRecord) IsRevoked() bool { return k != nil && k.RevokedAt != nil }

// IsExpired reports whether the key's optional expiry has passed at "now".
func (k *APIKeyRecord) IsExpired(now time.Time) bool {
	if k == nil || k.ExpiresAt == nil {
		return false
	}
	return now.After(*k.ExpiresAt)
}

// IsRotationExpired reports whether a scheduled rotation has landed: a key
// with RotatesAt set past "now" is treated as expired by the middleware,
// ending its grace period. Keys without RotatesAt are never rotation-expired.
func (k *APIKeyRecord) IsRotationExpired(now time.Time) bool {
	if k == nil || k.RotatesAt == nil {
		return false
	}
	return !now.Before(*k.RotatesAt)
}

// InRotationWarningWindow reports whether the key's scheduled rotation is
// within the supplied look-ahead window: now <= *RotatesAt <= now+window.
// Rotation already past "now" is NOT in the window (that's IsRotationExpired).
// Keys without RotatesAt return false.
func (k *APIKeyRecord) InRotationWarningWindow(now time.Time, window time.Duration) bool {
	if k == nil || k.RotatesAt == nil || window < 0 {
		return false
	}
	if now.After(*k.RotatesAt) {
		return false
	}
	return !k.RotatesAt.After(now.Add(window))
}

// GenerateAPIKey returns a fresh raw key and its lookup prefix. The raw key is
// the only place where the random secret exists in cleartext; the caller is
// responsible for storing only HashAPIKey(raw) and the returned prefix.
func GenerateAPIKey() (raw string, prefix string, err error) {
	// 5 bytes -> 8 base32 chars (no padding) for the prefix.
	prefixBytes := make([]byte, 5)
	if _, err := rand.Read(prefixBytes); err != nil {
		return "", "", err
	}
	prefix = b32.EncodeToString(prefixBytes)
	if len(prefix) != APIKeyPrefixLen {
		// Defensive: base32 of 5 bytes is always 8 chars; this never trips.
		return "", "", errors.New("api key: prefix encoding length mismatch")
	}

	// 32 bytes -> 52 base32 chars (no padding) for the random secret.
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", err
	}
	random := b32.EncodeToString(randomBytes)

	raw = APIKeyMarker + prefix + "_" + random
	return raw, prefix, nil
}

// HashAPIKey returns the SHA-256 digest of the raw key. The result is the
// 32-byte digest, suitable for direct insert into the BYTEA column. Callers
// MUST use crypto/subtle.ConstantTimeCompare for comparisons.
func HashAPIKey(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// IsAPIKey reports whether the supplied bearer token looks like a Weave API
// key. This is a cheap shape check used by the auth middleware to fork the
// token-validation path; the actual verification still happens in the API key
// repository lookup + constant-time hash compare.
func IsAPIKey(token string) bool {
	return strings.HasPrefix(token, APIKeyMarker)
}

// ParseAPIKey validates the structural shape of a raw key and returns its
// lookup prefix. It does NOT touch the database; the caller still has to
// look the prefix up and constant-time compare the hash.
func ParseAPIKey(raw string) (prefix string, err error) {
	if !strings.HasPrefix(raw, APIKeyMarker) {
		return "", ErrInvalidAPIKeyFormat
	}
	rest := strings.TrimPrefix(raw, APIKeyMarker)
	parts := strings.SplitN(rest, "_", 2)
	if len(parts) != 2 {
		return "", ErrInvalidAPIKeyFormat
	}
	prefix = parts[0]
	random := parts[1]
	if len(prefix) != APIKeyPrefixLen {
		return "", ErrInvalidAPIKeyFormat
	}
	if len(random) < 32 {
		return "", ErrInvalidAPIKeyFormat
	}
	return prefix, nil
}
