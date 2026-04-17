package developer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"strings"
	"time"
)

// OAuth bearer token format.
//
//	access-token  = "wvoa_" <prefix:8 base32> "_" <random:52 base32>
//	refresh-token = "wvor_" <prefix:8 base32> "_" <random:52 base32>
//
// The prefix is an O(1) DB lookup index; the random segment is never
// persisted, only its SHA-256 digest. The "wvo" marker lets the auth
// middleware fork to the OAuth code path BEFORE hitting the JWT verifier,
// parallel to how "wvk_" api keys are routed.
const (
	AccessTokenMarker  = "wvoa_"
	RefreshTokenMarker = "wvor_"

	OAuthPrefixLen = 8  // base32 chars in the lookup prefix
	OAuthRandomLen = 52 // base32 chars in the random segment (32 bytes)

	// DefaultAccessTokenTTL is the wall-clock lifetime of a freshly minted
	// access token. 1h is the Foundry default.
	DefaultAccessTokenTTL = 1 * time.Hour

	// DefaultRefreshTokenTTL is the wall-clock lifetime of a refresh token.
	// 30 days matches the login refresh policy.
	DefaultRefreshTokenTTL = 30 * 24 * time.Hour
)

// Token types for oauth_tokens.token_type.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// OAuth token errors.
var (
	ErrTokenNotFound      = errors.New("oauth token not found")
	ErrTokenRevoked       = errors.New("oauth token has been revoked")
	ErrTokenExpired       = errors.New("oauth token has expired")
	ErrInvalidTokenFormat = errors.New("invalid oauth token format")
)

// oauthB32 is non-padded uppercase base32, matching the api-key alphabet so
// operators who already work with wvk_ tokens don't need a new mental model.
var oauthB32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// OAuthToken is the persistent shape of an oauth_tokens row. The raw bearer
// string is never persisted; TokenHash stores the SHA-256 of the raw bytes
// and TokenPrefix is the structural lookup index.
type OAuthToken struct {
	ID          string
	TokenHash   []byte
	TokenPrefix string
	TokenType   string
	ClientID    string
	UserID      string // empty for client_credentials grants
	Scopes      []string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RevokedAt   *time.Time
}

// IsUsable returns nil when the token row is still valid, or a typed
// sentinel (ErrTokenExpired / ErrTokenRevoked) otherwise.
func (t *OAuthToken) IsUsable(now time.Time) error {
	if t == nil {
		return ErrTokenNotFound
	}
	if t.RevokedAt != nil {
		return ErrTokenRevoked
	}
	if now.After(t.ExpiresAt) {
		return ErrTokenExpired
	}
	return nil
}

// GenerateAccessToken mints a fresh access-token. The raw string is returned
// in cleartext (to be sent to the client exactly once); the returned prefix
// is what gets stored next to HashOAuthToken(raw) in the DB.
func GenerateAccessToken() (raw string, prefix string, err error) {
	return generateOAuthToken(AccessTokenMarker)
}

// GenerateRefreshToken mints a fresh refresh-token, same shape as
// GenerateAccessToken with the wvor_ marker.
func GenerateRefreshToken() (raw string, prefix string, err error) {
	return generateOAuthToken(RefreshTokenMarker)
}

func generateOAuthToken(marker string) (raw, prefix string, err error) {
	prefixBytes := make([]byte, 5)
	if _, err := rand.Read(prefixBytes); err != nil {
		return "", "", err
	}
	prefix = oauthB32.EncodeToString(prefixBytes)
	if len(prefix) != OAuthPrefixLen {
		return "", "", errors.New("oauth token: prefix encoding length mismatch")
	}
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", "", err
	}
	random := oauthB32.EncodeToString(randomBytes)
	raw = marker + prefix + "_" + random
	return raw, prefix, nil
}

// HashOAuthToken returns the SHA-256 digest of the raw bearer. 32-byte
// output; callers verifying MUST use subtle.ConstantTimeCompare.
func HashOAuthToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// IsOAuthAccessToken reports whether the bearer looks like an OAuth access
// token (wvoa_ marker). Used by the auth middleware to fork the bearer
// validation path.
func IsOAuthAccessToken(token string) bool {
	return strings.HasPrefix(token, AccessTokenMarker)
}

// IsOAuthRefreshToken reports whether the bearer looks like an OAuth refresh
// token (wvor_ marker).
func IsOAuthRefreshToken(token string) bool {
	return strings.HasPrefix(token, RefreshTokenMarker)
}

// ParseOAuthToken splits a raw token into its marker / prefix / random
// segments and returns only the prefix. Does NOT touch the database.
func ParseOAuthToken(raw string) (prefix string, err error) {
	var rest string
	switch {
	case strings.HasPrefix(raw, AccessTokenMarker):
		rest = strings.TrimPrefix(raw, AccessTokenMarker)
	case strings.HasPrefix(raw, RefreshTokenMarker):
		rest = strings.TrimPrefix(raw, RefreshTokenMarker)
	default:
		return "", ErrInvalidTokenFormat
	}
	parts := strings.SplitN(rest, "_", 2)
	if len(parts) != 2 {
		return "", ErrInvalidTokenFormat
	}
	prefix = parts[0]
	random := parts[1]
	if len(prefix) != OAuthPrefixLen {
		return "", ErrInvalidTokenFormat
	}
	if len(random) < 32 {
		return "", ErrInvalidTokenFormat
	}
	return prefix, nil
}

// OAuthTokenRepository persists and retrieves oauth_tokens rows.
type OAuthTokenRepository interface {
	Create(ctx context.Context, tok *OAuthToken) error
	GetByPrefix(ctx context.Context, prefix, tokenType string) ([]*OAuthToken, error)
	Revoke(ctx context.Context, id string, at time.Time) error
}

// ScopeIntersects reports whether any scope in `required` is present in
// `granted`. Foundry's OAuth surface treats scopes disjunctively: any match
// authorises the request. If `required` is empty the route has no scope
// requirement and every authenticated token passes.
func ScopeIntersects(granted, required []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(granted))
	for _, s := range granted {
		set[s] = struct{}{}
	}
	for _, s := range required {
		if _, ok := set[s]; ok {
			return true
		}
	}
	return false
}
