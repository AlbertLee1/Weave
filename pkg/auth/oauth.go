package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/liyang/weave/pkg/apierror"
)

// OAuthAccessTokenMarker is the bearer-prefix that identifies a Weave OAuth
// access token. Kept in sync (by value) with
// pkg/developer.AccessTokenMarker. It lives here too so the auth middleware
// can fork on token shape without importing pkg/developer (which itself
// depends on pkg/auth for UserFromContext — a circular import otherwise).
const OAuthAccessTokenMarker = "wvoa_"

// OAuthScopesAttributeKey is the User.Attributes key that carries the
// scopes granted to the request's access token. When the attribute is
// present the request was authenticated via an OAuth bearer; when absent
// the caller authenticated through any other mechanism (dev, JWT, API key)
// and per-route scope gates treat them as unrestricted.
const OAuthScopesAttributeKey = "oauth_scopes"

// OAuthClientIDAttributeKey is the User.Attributes key that carries the
// OAuth client_id that minted the access token. Used by US-144 to attribute
// usage metrics per application.
const OAuthClientIDAttributeKey = "oauth_client_id"

// ErrInvalidOAuthToken is the opaque error returned to the middleware when
// any step of the OAuth token validation pipeline fails. The validator
// should NOT leak which step failed — return this sentinel and let the
// middleware render a generic 401.
var ErrInvalidOAuthToken = errors.New("invalid oauth access token")

// OAuthPrincipal is the minimum identity information the middleware needs
// to build a User from a verified OAuth access token. pkg/developer knows
// how to resolve a raw token to this shape via its OAuthTokenRepository.
type OAuthPrincipal struct {
	UserID   string
	ClientID string
	Scopes   []string
}

// OAuthTokenValidator is the hook the middleware uses to validate an
// incoming OAuth bearer. Implementations MUST constant-time compare the
// hash of the supplied token against the stored digest and MUST reject
// expired / revoked tokens. A failed validation should return
// ErrInvalidOAuthToken (or any non-nil error — the middleware never
// surfaces the inner error message).
type OAuthTokenValidator interface {
	ValidateOAuthAccessToken(ctx context.Context, rawToken string) (*OAuthPrincipal, error)
}

// IsOAuthAccessToken reports whether a bearer token looks like a Weave
// OAuth access token (wvoa_ marker). Does NOT validate the token.
func IsOAuthAccessToken(tok string) bool {
	return strings.HasPrefix(tok, OAuthAccessTokenMarker)
}

// MiddlewareFull is the superset constructor that adds OAuth bearer
// validation to MiddlewareWithAPIKeys. When oauth is non-nil, any Bearer
// token starting with wvoa_ is routed to the validator BEFORE the mode
// switch, so OAuth access works uniformly across dev / jwt / token modes.
// When oauth is nil the middleware degrades identically to
// MiddlewareWithAPIKeys.
func MiddlewareFull(signer *JWTSigner, apiKeys APIKeyRepository, users UserRepository, resolver *RoleResolver, oauth OAuthTokenValidator) func(http.Handler) http.Handler {
	return MiddlewareFullWithRevocation(signer, apiKeys, users, resolver, oauth, nil)
}

// MiddlewareFullWithRevocation is MiddlewareFull plus a US-491 JTI
// revocation checker. A nil checker is equivalent to MiddlewareFull; the
// production wiring in cmd/server/main.go passes a *CachedRevocationChecker
// so revoked access tokens get rejected at the middleware boundary on the
// very next request after the admin revoke endpoint returns 200.
func MiddlewareFullWithRevocation(signer *JWTSigner, apiKeys APIKeyRepository, users UserRepository, resolver *RoleResolver, oauth OAuthTokenValidator, revoked RevocationChecker) func(http.Handler) http.Handler {
	inner := MiddlewareWithRevocation(signer, apiKeys, users, resolver, revoked)
	if oauth == nil {
		return inner
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := extractBearer(r)
			if IsOAuthAccessToken(tok) {
				handleOAuth(tok, oauth, next, w, r)
				return
			}
			inner(next).ServeHTTP(w, r)
		})
	}
}

// handleOAuth resolves a wvoa_ bearer to a User and passes it down the
// chain. Errors always render a generic 401 to avoid leaking which step
// failed (unknown prefix vs. hash mismatch vs. expired — all look the same
// to the client).
func handleOAuth(tok string, oauth OAuthTokenValidator, next http.Handler, w http.ResponseWriter, r *http.Request) {
	principal, err := oauth.ValidateOAuthAccessToken(r.Context(), tok)
	if err != nil || principal == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("InvalidAccessToken", map[string]string{
			"reason": "access token failed validation",
		}))
		return
	}
	u := &User{ID: principal.UserID}
	if u.ID == "" {
		// client_credentials grants have no end-user; expose the client as
		// the identity so handlers that log User.ID still get a meaningful
		// string. "app:" prefix mirrors the "user:" convention.
		u.ID = "app:" + principal.ClientID
	}
	u.Attributes = map[string]any{
		OAuthScopesAttributeKey:   append([]string(nil), principal.Scopes...),
		OAuthClientIDAttributeKey: principal.ClientID,
	}
	ctx := WithUser(r.Context(), u)
	next.ServeHTTP(w, r.WithContext(ctx))
}

// OAuthScopes returns the scopes granted to the request's access token.
// Returns nil when the caller did not authenticate via an OAuth bearer
// (dev mode, JWT, API key).
func OAuthScopes(ctx context.Context) []string {
	u := UserFromContext(ctx)
	if u == nil || u.Attributes == nil {
		return nil
	}
	raw, ok := u.Attributes[OAuthScopesAttributeKey]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// RequireOAuthScope returns middleware that rejects requests whose access
// token does not carry at least one of the supplied scopes.
//
// When the caller authenticated WITHOUT an OAuth bearer — dev mode, JWT,
// or an API key — the middleware passes through. This preserves the
// existing behaviour of every non-OAuth-scoped surface: you only get
// scope-gated when you explicitly sent a scoped access token.
func RequireOAuthScope(scopes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			granted := OAuthScopes(r.Context())
			if granted == nil {
				// Non-OAuth caller; the route still runs through RBAC /
				// ontology-scope middleware elsewhere.
				next.ServeHTTP(w, r)
				return
			}
			if !scopeIntersects(granted, scopes) {
				apierror.WriteJSON(w, apierror.NewPermissionDenied("InsufficientScope", map[string]string{
					"required": strings.Join(scopes, " "),
				}))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func scopeIntersects(granted, required []string) bool {
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
