package developer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// Authorization-code constants.
//
// A code is a short-lived single-use token returned by the /oauth/authorize
// consent step. The client redeems it at /oauth/token along with a matching
// code_verifier (PKCE) to receive an access token.
const (
	// AuthCodePrefix — leading "oac_" helps operators and logs tell the
	// authorization code apart from the subsequent access / refresh tokens
	// (wvoa_ / wvor_). It is opaque to the OAuth spec.
	AuthCodePrefix = "oac_"

	// AuthCodeTTL is the wall-clock lifetime of an authorization code. The
	// OAuth 2.0 spec recommends <= 10 minutes (RFC 6749 §4.1.2); five is
	// enough for a single redirect hop.
	AuthCodeTTL = 5 * time.Minute

	// PKCEMethodS256 is the only code_challenge_method we accept. Plain
	// is rejected on purpose — downgrading to plain defeats the point of
	// PKCE when the client is a single-page app.
	PKCEMethodS256 = "S256"
)

// AuthorizationCode errors surfaced by repository and PKCE helpers.
var (
	ErrAuthorizationCodeNotFound = errors.New("authorization code not found")
	ErrAuthorizationCodeExpired  = errors.New("authorization code expired")
	ErrAuthorizationCodeConsumed = errors.New("authorization code already consumed")
	ErrPKCEChallengeMismatch     = errors.New("pkce code_verifier does not match code_challenge")
	ErrUnsupportedPKCEMethod     = errors.New("unsupported code_challenge_method (only S256 is accepted)")
	ErrInvalidPKCEVerifier       = errors.New("invalid code_verifier")
	ErrInvalidRedirectURI        = errors.New("redirect_uri is not registered for this client")
)

// AuthorizationCode is the persistent shape of an oauth_authorization_codes
// row. The code is the opaque bearer handed to the caller via redirect;
// consumed_at is stamped by the token endpoint on first use to make the
// exchange single-use.
type AuthorizationCode struct {
	ID                  string
	Code                string
	ClientID            string
	UserID              string
	RedirectURI         string
	Scopes              []string
	CodeChallenge       string
	CodeChallengeMethod string
	CreatedAt           time.Time
	ExpiresAt           time.Time
	ConsumedAt          *time.Time
}

// IsUsable returns nil when the code is still redeemable, or a typed
// sentinel error (ErrAuthorizationCodeExpired / ErrAuthorizationCodeConsumed)
// describing why it is not. Caller checks this before comparing PKCE data.
func (a *AuthorizationCode) IsUsable(now time.Time) error {
	if a == nil {
		return ErrAuthorizationCodeNotFound
	}
	if a.ConsumedAt != nil {
		return ErrAuthorizationCodeConsumed
	}
	if now.After(a.ExpiresAt) {
		return ErrAuthorizationCodeExpired
	}
	return nil
}

// GenerateAuthorizationCode returns a fresh opaque authorization code. It
// embeds AuthCodePrefix so logs / DB grep pick it out easily.
func GenerateAuthorizationCode() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	enc := b32.EncodeToString(buf)
	return AuthCodePrefix + enc, nil
}

// ComputePKCEChallenge returns the S256 PKCE code_challenge for the given
// code_verifier. Per RFC 7636 §4.2:
//
//	code_challenge = BASE64URL-ENCODE(SHA256(ASCII(code_verifier)))
//
// where BASE64URL-ENCODE strips padding. Callers that only need to verify
// an incoming verifier should use VerifyPKCE instead; this helper exists so
// tests can pre-compute the challenge matching a known verifier.
func ComputePKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// VerifyPKCE reports whether the supplied code_verifier matches the stored
// code_challenge under the given method. Only S256 is supported. The
// comparison is constant-time so a malicious client cannot time-measure its
// way to a matching challenge. The verifier is also shape-checked to follow
// RFC 7636 §4.1 (43–128 characters from the unreserved set) because a
// ten-byte verifier is trivially brute-forceable even with a correct SHA-256
// pipeline.
func VerifyPKCE(challenge, verifier, method string) error {
	if method == "" {
		method = PKCEMethodS256
	}
	if method != PKCEMethodS256 {
		return ErrUnsupportedPKCEMethod
	}
	if err := validatePKCEVerifier(verifier); err != nil {
		return err
	}
	computed := ComputePKCEChallenge(verifier)
	if subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) != 1 {
		return ErrPKCEChallengeMismatch
	}
	return nil
}

// validatePKCEVerifier enforces RFC 7636 §4.1: verifier is 43..128 chars,
// drawn from [A-Z] / [a-z] / [0-9] / "-" / "." / "_" / "~".
func validatePKCEVerifier(v string) error {
	if len(v) < 43 || len(v) > 128 {
		return ErrInvalidPKCEVerifier
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-', c == '.', c == '_', c == '~':
		default:
			return ErrInvalidPKCEVerifier
		}
	}
	return nil
}

// ValidateRedirectURI reports whether the supplied URI matches any of the
// application's registered redirect_uris. Exact string match per OAuth 2.0
// §3.1.2.2 ("the authorization server SHOULD require the client to provide
// its redirection URI and ... validate the URI against the registered
// values"). Returns nil if the app has zero registered URIs only when the
// caller also passes an empty URI — an unregistered app cannot redirect
// anywhere.
func ValidateRedirectURI(app *Application, redirectURI string) error {
	if app == nil {
		return ErrInvalidRedirectURI
	}
	redirectURI = strings.TrimSpace(redirectURI)
	for _, registered := range app.RedirectURIs {
		if registered == redirectURI {
			return nil
		}
	}
	return ErrInvalidRedirectURI
}

// AuthorizationCodeRepository persists and retrieves authorization codes.
// The token endpoint's exchange path also calls MarkConsumed to make the
// code single-use.
type AuthorizationCodeRepository interface {
	Create(ctx context.Context, code *AuthorizationCode) error
	GetByCode(ctx context.Context, code string) (*AuthorizationCode, error)
	MarkConsumed(ctx context.Context, id string, at time.Time) error
}
