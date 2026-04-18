// Package auth — OIDC back-channel logout handler (US-255).
//
// Back-channel logout is the server-to-server "the IdP says this user
// logged out upstream" path defined by OpenID Connect Back-Channel Logout
// 1.0. The IdP POSTs a signed Logout Token (a JWT) to a registered SP
// endpoint; the SP validates the token and then terminates every local
// session belonging to the named user.
//
// Unlike front-channel logout (which happens in the browser via iframes
// loading each RP's logout URL), back-channel logout has no browser
// involvement — there is no state cookie to match, no redirect to follow.
// The verification hinges entirely on the JWT signature + a small set of
// logout-specific claim shape requirements.
//
// Endpoint:
//
//	POST /api/auth/oidc/back-channel-logout
//	Content-Type: application/x-www-form-urlencoded
//	Body:         logout_token=<JWT>
//
// On success the handler:
//  1. revokes every outstanding refresh token for the user
//     (RefreshService.RevokeAllForUser → reason="oidc_back_channel_logout")
//  2. deletes every active session row for the user
//     (SessionStore.DeleteAllForUser)
//  3. returns 200 OK with Cache-Control: no-store per spec §2.8
//
// The handler is idempotent: unknown users return 200 rather than leaking
// which subjects exist in the SP's user table.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/liyang/weave/pkg/apierror"
)

// BackChannelLogoutEventKey is the event identifier the OIDC spec
// (§2.4) requires inside the logout_token's `events` claim. Exported so
// IdP-side test fixtures can seed the exact key.
const BackChannelLogoutEventKey = "http://schemas.openid.net/event/backchannel-logout"

// OIDCBackChannelLogoutHandler wires POST /api/auth/oidc/back-channel-logout.
// The collaborators are the same set the OIDCHandler already owns — we
// reuse the provider's JWT verifier (same issuer + same JWKS) so that if
// the IdP's signing keys rotate the logout path picks up the new keys
// automatically alongside the login path.
type OIDCBackChannelLogoutHandler struct {
	deps OIDCBackChannelLogoutDeps
}

// OIDCBackChannelLogoutDeps groups the collaborators needed to validate a
// logout token + terminate the named user's local state.
type OIDCBackChannelLogoutDeps struct {
	// Verifier validates the JWT signature + iss / aud / exp. Shared with
	// the OIDC login handler so key rotation happens in one place.
	Verifier OIDCVerifier
	// ClientID is the expected `aud` value for logout tokens. Logout
	// tokens are audience-scoped to the RP, same as id_tokens, so we
	// accept only tokens whose aud matches our configured client.
	ClientID string
	// Users resolves the subject claim (sub or sid or email) to a
	// Weave UserRecord. nil disables user lookup (handler returns 200
	// idempotently — useful for degraded-mode routers).
	Users UserRepository
	// SessionStore is where the handler calls DeleteAllForUser when a
	// valid logout token names a known user. Nil = no session teardown.
	SessionStore SessionStore
	// RefreshService revokes every outstanding refresh token belonging
	// to the user. Nil = no refresh revocation.
	RefreshService *RefreshService
}

// oidcLogoutTokenClaims is the narrow view of a logout_token's claims the
// handler inspects. Provider-specific extensions (azp, at_hash, ...) are
// preserved in the raw token but ignored here.
type oidcLogoutTokenClaims struct {
	Subject string                 `json:"sub"`
	SID     string                 `json:"sid"`
	Email   string                 `json:"email"`
	JTI     string                 `json:"jti"`
	Nonce   string                 `json:"nonce"`
	Events  map[string]interface{} `json:"events"`
}

// NewOIDCBackChannelLogoutHandler constructs the handler. The caller owns
// the Verifier — production wiring reuses the one NewOIDCDepsFromProvider
// built for the login path; tests substitute a stub.
func NewOIDCBackChannelLogoutHandler(deps OIDCBackChannelLogoutDeps) *OIDCBackChannelLogoutHandler {
	return &OIDCBackChannelLogoutHandler{deps: deps}
}

// RegisterRoutes attaches POST /api/auth/oidc/back-channel-logout.
func (h *OIDCBackChannelLogoutHandler) RegisterRoutes(mux interface {
	Method(method, pattern string, handler http.Handler)
}) {
	mux.Method(http.MethodPost, "/api/auth/oidc/back-channel-logout", http.HandlerFunc(h.ServeHTTP))
}

// ServeHTTP implements http.Handler. Validates the logout_token and
// terminates the named user's local state.
func (h *OIDCBackChannelLogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MethodNotAllowed", map[string]string{
			"reason": "POST required",
		}))
		return
	}
	if err := r.ParseForm(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("OIDCLogoutFormParseFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	raw := strings.TrimSpace(r.Form.Get("logout_token"))
	if raw == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("OIDCLogoutMissingToken", map[string]string{
			"reason": "logout_token form field is required",
		}))
		return
	}

	if h.deps.Verifier == nil {
		apierror.WriteJSON(w, apierror.NewInternal("OIDCLogoutUnavailable", map[string]string{
			"reason": "verifier not wired",
		}))
		return
	}

	tok, err := h.deps.Verifier.Verify(r.Context(), raw)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCLogoutTokenInvalid", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	var claims oidcLogoutTokenClaims
	if err := tok.Claims(&claims); err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCLogoutClaimsInvalid", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if err := validateLogoutTokenClaims(&claims); err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCLogoutClaimsInvalid", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	// Resolve the user. Try email first (most RPs include it), fall back
	// to `sub` interpreted as either an email or a "user:<email>" id.
	// Unknown users intentionally short-circuit to 200 — the IdP doesn't
	// need to know whether we had a session for this subject (and
	// disclosing it would help attackers probe the user store).
	user := h.resolveUser(r.Context(), &claims)
	if user != nil {
		if h.deps.RefreshService != nil {
			_ = h.deps.RefreshService.RevokeAllForUser(r.Context(), user.ID, "oidc_back_channel_logout")
		}
		if h.deps.SessionStore != nil {
			_ = h.deps.SessionStore.DeleteAllForUser(r.Context(), user.ID)
		}
	}

	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{}`))
}

// resolveUser walks the claim identifiers in priority order. Returns nil
// when no match is found so the caller can treat "unknown subject" as a
// silent no-op.
func (h *OIDCBackChannelLogoutHandler) resolveUser(ctx context.Context, claims *oidcLogoutTokenClaims) *UserRecord {
	if h.deps.Users == nil {
		return nil
	}
	candidates := oidcLogoutSubjectCandidates(claims)
	for _, email := range candidates.emails {
		if u, err := h.deps.Users.GetUserByEmail(ctx, email); err == nil && u != nil {
			return u
		}
	}
	for _, id := range candidates.userIDs {
		if u, err := h.deps.Users.GetUserByID(ctx, id); err == nil && u != nil {
			return u
		}
	}
	return nil
}

// subjectCandidates is the ordered list of identifiers extracted from the
// logout token. We try email lookups first (the canonical path used by
// the password / OIDC / SAML login handlers) then user-id lookups
// synthesised from sub / sid.
type subjectCandidates struct {
	emails  []string
	userIDs []string
}

func oidcLogoutSubjectCandidates(claims *oidcLogoutTokenClaims) subjectCandidates {
	out := subjectCandidates{}
	if e := strings.ToLower(strings.TrimSpace(claims.Email)); e != "" {
		out.emails = appendUnique(out.emails, e)
	}
	if looksLikeEmail(claims.Subject) {
		out.emails = appendUnique(out.emails, strings.ToLower(strings.TrimSpace(claims.Subject)))
	}
	// user:<email> is the id convention every login handler uses, so
	// mapping sub ("alice@example.com") to "user:alice@example.com" is
	// the right fallback when the IdP set sub == email.
	if looksLikeEmail(claims.Subject) {
		out.userIDs = appendUnique(out.userIDs, "user:"+strings.ToLower(strings.TrimSpace(claims.Subject)))
	}
	// Raw sub as user id (rare but some self-hosted IdPs configure it).
	if s := strings.TrimSpace(claims.Subject); s != "" {
		out.userIDs = appendUnique(out.userIDs, s)
	}
	return out
}

func looksLikeEmail(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	at := strings.IndexByte(s, '@')
	return at > 0 && at < len(s)-1
}

func appendUnique(ss []string, s string) []string {
	for _, existing := range ss {
		if existing == s {
			return ss
		}
	}
	return append(ss, s)
}

// validateLogoutTokenClaims enforces the shape requirements from
// OpenID Connect Back-Channel Logout 1.0 §2.4:
//
//   - events claim MUST contain the logout event key
//   - nonce claim MUST NOT be present
//   - sub OR sid MUST be present
//
// Signature / iss / aud / exp are already validated upstream by the
// verifier; those checks are identical to the id_token path.
func validateLogoutTokenClaims(c *oidcLogoutTokenClaims) error {
	if c.Nonce != "" {
		return errors.New("logout_token must not include a nonce claim")
	}
	if len(c.Events) == 0 {
		return errors.New("logout_token missing events claim")
	}
	if _, ok := c.Events[BackChannelLogoutEventKey]; !ok {
		return errors.New("logout_token events claim missing back-channel logout key")
	}
	if strings.TrimSpace(c.Subject) == "" && strings.TrimSpace(c.SID) == "" {
		return errors.New("logout_token must include sub or sid")
	}
	return nil
}

// Compile-time proof the handler satisfies http.Handler.
var _ http.Handler = (*OIDCBackChannelLogoutHandler)(nil)
