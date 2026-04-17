// Package developer implements the Developer Console backend: OAuth
// application registration (US-141), the OAuth 2.0 authorization-code flow
// (US-142) and per-application usage metrics (US-144).
package developer

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"strings"
	"time"
)

// Credential format constants. client_id is a public identifier
// (clientIDPrefix + 24 base32 chars) and client_secret is a longer opaque
// bearer value (clientSecretPrefix + 52 base32 chars ~ 256 bits of entropy).
// The prefixes exist purely to help an operator tell them apart by eye.
const (
	ClientIDPrefix     = "wapp_"
	ClientSecretPrefix = "wsec_"

	clientIDRandomLen     = 24
	clientSecretRandomLen = 52
)

// b32 is non-padded uppercase base32 (RFC 4648). Matching the api-key module
// keeps operator muscle memory aligned across "wvk_" / "wapp_" / "wsec_".
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// ErrInvalidClientSecretFormat is returned by VerifyClientSecret when the
// input does not match the expected "wsec_..." shape. Callers should treat
// this as a permission failure (not a server error) to avoid leaking shape
// information.
var ErrInvalidClientSecretFormat = errors.New("invalid client secret format")

// Application is the persistent shape of a registered OAuth application.
//
// ClientSecret is ONLY ever populated on the response returned from
// CreateApplication; the secret is never stored in the DB in plaintext and
// can never be retrieved afterwards. Callers who need to verify a submitted
// secret call VerifyClientSecret(submitted, row.ClientSecretHash).
type Application struct {
	ID                string
	Name              string
	Description       string
	ClientID          string
	ClientSecretHash  []byte
	RedirectURIs      []string
	Scopes            []string
	CreatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// GenerateClientID returns a fresh, opaque public identifier for an
// application. Each call produces a unique string; collisions on the DB's
// UNIQUE constraint are statistically negligible (24 chars of base32 ≈ 120
// bits of entropy).
func GenerateClientID() (string, error) {
	// 15 bytes -> 24 base32 chars (no padding).
	buf := make([]byte, 15)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	enc := b32.EncodeToString(buf)
	if len(enc) != clientIDRandomLen {
		return "", errors.New("client_id: encoding length mismatch")
	}
	return ClientIDPrefix + enc, nil
}

// GenerateClientSecret returns a fresh opaque bearer secret. It is returned
// in cleartext to the caller of CreateApplication once; persist only
// HashClientSecret(secret).
func GenerateClientSecret() (string, error) {
	// 32 bytes -> 52 base32 chars (no padding) ≈ 256 bits of entropy.
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	enc := b32.EncodeToString(buf)
	if len(enc) != clientSecretRandomLen {
		return "", errors.New("client_secret: encoding length mismatch")
	}
	return ClientSecretPrefix + enc, nil
}

// HashClientSecret returns the SHA-256 digest of the cleartext secret. The
// 32-byte digest is what lands in the DB (client_secret_hash BYTEA). Callers
// verifying a submitted secret MUST use crypto/subtle.ConstantTimeCompare.
func HashClientSecret(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// ValidateClientSecretShape reports whether the supplied bearer string
// matches the "wsec_..." structural contract. The actual secret check still
// runs against the hash; this is just a cheap pre-filter for the OAuth token
// endpoint.
func ValidateClientSecretShape(raw string) error {
	if !strings.HasPrefix(raw, ClientSecretPrefix) {
		return ErrInvalidClientSecretFormat
	}
	random := strings.TrimPrefix(raw, ClientSecretPrefix)
	if len(random) < 32 {
		return ErrInvalidClientSecretFormat
	}
	return nil
}
