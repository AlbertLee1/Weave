// Package auth — OIDC handler (US-246).
//
// The OIDC handler exposes two HTTP endpoints that together implement the
// OpenID Connect Authorization Code flow against an external provider
// (Keycloak, Okta, Google, Auth0, ...):
//
//	GET  /api/auth/oidc/login     — redirect to the provider's authorize URL
//	GET  /api/auth/oidc/callback  — exchange code → verify id_token → mint Weave session
//
// On a successful callback the handler upserts the user into
// UserRepository from the ID token claims (sub, email, name) and issues a
// Weave access + refresh token pair (the exact same shape LoginHandler
// returns) so downstream API calls keep going through the existing JWT
// middleware without any OIDC-specific plumbing.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/liyang/weave/pkg/apierror"
)

// OIDCConfig is the minimum configuration the OIDC handler needs. All fields
// except Scopes are required; Scopes defaults to ["openid","email","profile"]
// when empty.
type OIDCConfig struct {
	// IssuerURL is the provider's canonical issuer (e.g.
	// "https://keycloak.example.com/realms/master"). Used for discovery
	// (.well-known/openid-configuration) and ID-token issuer validation.
	IssuerURL    string
	ClientID     string
	ClientSecret string
	// RedirectURL is the absolute URL of the /api/auth/oidc/callback endpoint
	// as the provider should redirect back to after authentication.
	RedirectURL string
	// Scopes requested from the provider. Defaults to
	// ["openid","email","profile"] when empty. "openid" is force-prepended
	// if the caller's list omits it.
	Scopes []string
	// SuccessRedirectURL, when non-empty, makes the callback emit a 302 to
	// this URL with access/refresh tokens appended as URL-fragment query
	// params (e.g. "/#access_token=...&refresh_token=..."). When empty the
	// callback returns a LoginResponse JSON body identical to the
	// password-login endpoint so SPAs that prefer to consume JSON can.
	SuccessRedirectURL string
}

// OIDCVerifier is the subset of *oidc.IDTokenVerifier the handler uses.
// Decoupling through an interface keeps the callback path unit-testable
// without standing up an external OP: tests substitute a stub that returns
// a pre-built *oidc.IDToken.
type OIDCVerifier interface {
	Verify(ctx context.Context, rawIDToken string) (*oidc.IDToken, error)
}

// OIDCTokenExchanger is the subset of *oauth2.Config the handler uses to
// convert an authorization code into a token response. Tests substitute a
// stub that returns a canned *oauth2.Token with a raw id_token extra.
type OIDCTokenExchanger interface {
	Exchange(ctx context.Context, code string, opts ...oauth2.AuthCodeOption) (*oauth2.Token, error)
	AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string
}

// OIDCClaims is the narrow set of claims the handler reads from an ID token.
// Providers carry many more; we only need subject + email + (optional) name.
type OIDCClaims struct {
	Subject       string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// OIDCHandlerDeps groups the collaborators an OIDCHandler needs. The auth
// package already has a working LoginHandler that mints session tokens from
// a UserRecord; the callback delegates to the same machinery so there is
// exactly one path that turns "we know who you are" into JWT+refresh.
type OIDCHandlerDeps struct {
	Config         OIDCConfig
	Exchanger      OIDCTokenExchanger
	Verifier       OIDCVerifier
	Users          UserRepository
	Resolver       *RoleResolver
	Signer         *JWTSigner
	RefreshService *RefreshService
	MarkingRepo    MarkingRepository
	// Now returns the current time; defaults to time.Now. Injectable for
	// deterministic state-cookie expiry in tests.
	Now func() time.Time
}

// OIDCHandler wires the /api/auth/oidc/{login,callback} endpoints.
type OIDCHandler struct {
	deps OIDCHandlerDeps
}

// stateCookieName carries the opaque state value between the login-redirect
// and callback to defend against CSRF on the authorization response.
const stateCookieName = "weave_oidc_state"

// stateCookieMaxAge is how long a state cookie is valid. The auth flow
// completes in seconds; ten minutes is a generous upper bound that still
// invalidates abandoned redirects.
const stateCookieMaxAge = 10 * 60

// DefaultOIDCScopes are the scopes requested when OIDCConfig.Scopes is empty.
var DefaultOIDCScopes = []string{oidc.ScopeOpenID, "email", "profile"}

// NewOIDCHandler constructs a handler. The caller owns the Verifier /
// Exchanger — in production they come from a real provider via NewOIDCDeps,
// in tests they are stubs.
func NewOIDCHandler(deps OIDCHandlerDeps) *OIDCHandler {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &OIDCHandler{deps: deps}
}

// RegisterRoutes attaches /api/auth/oidc/login and /api/auth/oidc/callback
// to the supplied mux. Kept as a method so main.go can mount the handler
// alongside the other public auth routes.
func (h *OIDCHandler) RegisterRoutes(mux interface {
	Method(method, pattern string, handler http.Handler)
}) {
	mux.Method(http.MethodGet, "/api/auth/oidc/login", http.HandlerFunc(h.Login))
	mux.Method(http.MethodGet, "/api/auth/oidc/callback", http.HandlerFunc(h.Callback))
}

// Login generates a fresh state, stores it in a short-lived HTTP-only cookie,
// and redirects the caller to the provider's authorize URL.
func (h *OIDCHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MethodNotAllowed", map[string]string{
			"reason": "GET required",
		}))
		return
	}
	state, err := newRandomState()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("OIDCStateFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   stateCookieMaxAge,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
	})
	authURL := h.deps.Exchanger.AuthCodeURL(state)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback handles the provider's redirect. It:
//  1. validates the state query param against the cookie planted by /login,
//  2. exchanges the code at the token endpoint,
//  3. verifies the returned id_token against the provider JWKS,
//  4. upserts a UserRecord from the claims,
//  5. mints a Weave access + refresh token pair (same shape as LoginResponse).
func (h *OIDCHandler) Callback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MethodNotAllowed", map[string]string{
			"reason": "GET required",
		}))
		return
	}

	// Provider-signalled errors land on the callback with ?error=... — surface
	// them verbatim so the operator can diagnose misconfigured apps.
	if providerErr := r.URL.Query().Get("error"); providerErr != "" {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCProviderError", map[string]string{
			"error":            providerErr,
			"errorDescription": r.URL.Query().Get("error_description"),
		}))
		return
	}

	state := r.URL.Query().Get("state")
	cookie, err := r.Cookie(stateCookieName)
	if err != nil || cookie.Value == "" || state == "" || cookie.Value != state {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCStateMismatch", map[string]string{
			"reason": "state query parameter does not match session cookie",
		}))
		return
	}
	// One-shot cookie: clear it immediately so a replayed callback can't
	// re-exchange the code.
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Path: "/", MaxAge: -1})

	code := r.URL.Query().Get("code")
	if code == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("OIDCMissingCode", map[string]string{
			"reason": "code query parameter is required",
		}))
		return
	}

	ctx := r.Context()
	tok, err := h.deps.Exchanger.Exchange(ctx, code)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCTokenExchangeFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCMissingIDToken", map[string]string{
			"reason": "provider did not return an id_token",
		}))
		return
	}
	idToken, err := h.deps.Verifier.Verify(ctx, rawID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCIDTokenInvalid", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	var claims OIDCClaims
	if err := idToken.Claims(&claims); err != nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCClaimsInvalid", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if claims.Subject == "" || claims.Email == "" {
		apierror.WriteJSON(w, apierror.NewUnauthorized("OIDCClaimsIncomplete", map[string]string{
			"reason": "id_token missing sub or email",
		}))
		return
	}

	user, err := h.upsertUser(ctx, &claims)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("OIDCUserUpsertFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	resp, err := h.issueSession(ctx, user)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("OIDCSessionFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	if h.deps.Config.SuccessRedirectURL != "" {
		http.Redirect(w, r, buildSuccessRedirect(h.deps.Config.SuccessRedirectURL, resp), http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// upsertUser resolves the UserRecord for the claim set, creating a new one
// on first sight of the email. The ID is derived from the email (same
// "user:<email>" convention BootstrapAdmin uses) so downstream RBAC lookups
// on user.ID keep working.
func (h *OIDCHandler) upsertUser(ctx context.Context, claims *OIDCClaims) (*UserRecord, error) {
	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		return nil, errors.New("email is empty")
	}
	if existing, err := h.deps.Users.GetUserByEmail(ctx, email); err == nil {
		// Keep the existing user's id/password_hash stable; only patch the
		// display name when the provider supplies a fresher value.
		if claims.Name != "" && existing.Name != claims.Name {
			existing.Name = claims.Name
		}
		return existing, nil
	} else if !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}
	rec := &UserRecord{
		ID:    "user:" + email,
		Email: email,
		Name:  claims.Name,
	}
	if err := h.deps.Users.CreateUser(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// issueSession mirrors the tail of LoginHandler.ServeHTTP: resolve fresh
// role grants, resolve user markings, sign an access token, generate a
// refresh token, and return the standard LoginResponse.
func (h *OIDCHandler) issueSession(ctx context.Context, user *UserRecord) (*LoginResponse, error) {
	global, scoped, err := h.deps.Resolver.Resolve(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("role resolve: %w", err)
	}
	var markings []string
	if h.deps.MarkingRepo != nil {
		markings, err = h.deps.MarkingRepo.GetUserMarkings(ctx, user.ID)
		if err != nil {
			return nil, fmt.Errorf("marking resolve: %w", err)
		}
	}
	access, err := h.deps.Signer.Sign(SignInput{
		UserID:        user.ID,
		Email:         user.Email,
		Name:          user.Name,
		Roles:         global,
		OntologyRoles: scoped,
		Markings:      markings,
	})
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}
	refreshPlain, _, err := h.deps.RefreshService.Generate(ctx, user.ID, "")
	if err != nil {
		return nil, fmt.Errorf("refresh: %w", err)
	}
	ttl := 15 * time.Minute
	if h.deps.Signer != nil && h.deps.Signer.ttl > 0 {
		ttl = h.deps.Signer.ttl
	}
	return &LoginResponse{
		AccessToken:  access,
		RefreshToken: refreshPlain,
		TokenType:    "Bearer",
		ExpiresIn:    int(ttl.Seconds()),
		User: LoginUser{
			ID:            user.ID,
			Email:         user.Email,
			Name:          user.Name,
			Roles:         emptyIfNilStrings(global),
			OntologyRoles: emptyIfNilMap(scoped),
		},
	}, nil
}

// newRandomState returns a URL-safe random string suitable for use as an
// OAuth2 state/nonce. 32 bytes = 256 bits of entropy.
func newRandomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// buildSuccessRedirect appends access+refresh tokens as query-string params
// to the configured success URL. Callers typically point this at an SPA
// route that reads the tokens out of location.search and stashes them in
// local storage.
func buildSuccessRedirect(base string, resp *LoginResponse) string {
	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	return fmt.Sprintf(
		"%s%saccess_token=%s&refresh_token=%s&token_type=Bearer&expires_in=%d",
		base, sep, resp.AccessToken, resp.RefreshToken, resp.ExpiresIn,
	)
}

// NewOIDCDepsFromProvider builds production-ready Exchanger + Verifier from
// an oidc.Provider + OIDCConfig. Callers should call oidc.NewProvider(ctx,
// issuer) first (that's the discovery hit) and pass the result here.
func NewOIDCDepsFromProvider(provider *oidc.Provider, cfg OIDCConfig) (OIDCTokenExchanger, OIDCVerifier) {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = DefaultOIDCScopes
	} else if !containsString(scopes, oidc.ScopeOpenID) {
		scopes = append([]string{oidc.ScopeOpenID}, scopes...)
	}
	oauthCfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	return oauthCfg, verifier
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
