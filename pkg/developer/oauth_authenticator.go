package developer

import (
	"context"
	"crypto/subtle"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// OAuthAuthenticator adapts an OAuthTokenRepository into the
// auth.OAuthTokenValidator contract the auth middleware consumes. It
// handles the full validation pipeline: shape parse, prefix lookup, hash
// compare, expiry / revocation check, then returns an auth.OAuthPrincipal
// carrying the scopes the request should be evaluated under.
type OAuthAuthenticator struct {
	tokens OAuthTokenRepository
	now    func() time.Time
}

// NewOAuthAuthenticator wires a repository into the middleware contract.
// Callers pass time.Now unless they need a controllable clock in tests.
func NewOAuthAuthenticator(tokens OAuthTokenRepository) *OAuthAuthenticator {
	return &OAuthAuthenticator{tokens: tokens, now: time.Now}
}

// WithClock overrides the time source. Exposed only for tests.
func (a *OAuthAuthenticator) WithClock(now func() time.Time) *OAuthAuthenticator {
	a.now = now
	return a
}

// ValidateOAuthAccessToken resolves a raw wvoa_ bearer to an
// auth.OAuthPrincipal, or returns auth.ErrInvalidOAuthToken (the only
// error the middleware surfaces) on any failure.
func (a *OAuthAuthenticator) ValidateOAuthAccessToken(ctx context.Context, raw string) (*auth.OAuthPrincipal, error) {
	if !IsOAuthAccessToken(raw) {
		return nil, auth.ErrInvalidOAuthToken
	}
	prefix, err := ParseOAuthToken(raw)
	if err != nil {
		return nil, auth.ErrInvalidOAuthToken
	}
	candidates, err := a.tokens.GetByPrefix(ctx, prefix, TokenTypeAccess)
	if err != nil || len(candidates) == 0 {
		return nil, auth.ErrInvalidOAuthToken
	}
	want := HashOAuthToken(raw)
	now := a.now()
	for _, tok := range candidates {
		if subtle.ConstantTimeCompare(tok.TokenHash, want) != 1 {
			continue
		}
		if err := tok.IsUsable(now); err != nil {
			return nil, auth.ErrInvalidOAuthToken
		}
		return &auth.OAuthPrincipal{
			UserID:   tok.UserID,
			ClientID: tok.ClientID,
			Scopes:   append([]string(nil), tok.Scopes...),
		}, nil
	}
	return nil, auth.ErrInvalidOAuthToken
}

var _ auth.OAuthTokenValidator = (*OAuthAuthenticator)(nil)
