package developer

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/auth"
)

// OAuthHandler implements /oauth/authorize (GET + POST) and /oauth/token
// (POST). It owns only HTTP-level orchestration — code generation, PKCE
// verification, and persistence live in the sibling modules.
type OAuthHandler struct {
	apps        ApplicationRepository
	codes       AuthorizationCodeRepository
	tokens      OAuthTokenRepository
	accessTTL   time.Duration
	refreshTTL  time.Duration
	authCodeTTL time.Duration
	now         func() time.Time
	consentTmpl *template.Template
}

// OAuthHandlerOptions tunes the handler's time / TTL knobs. All fields are
// optional; zero values fall back to the package defaults.
type OAuthHandlerOptions struct {
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AuthCodeTTL     time.Duration
	Now             func() time.Time
}

// NewOAuthHandler wires the three repositories together with default TTLs
// (1h access, 30d refresh, 5m authorization code).
func NewOAuthHandler(apps ApplicationRepository, codes AuthorizationCodeRepository, tokens OAuthTokenRepository) *OAuthHandler {
	return NewOAuthHandlerWithOptions(apps, codes, tokens, OAuthHandlerOptions{})
}

// NewOAuthHandlerWithOptions is the injection variant — tests pass a
// deterministic Now() clock so expiry assertions don't flake.
func NewOAuthHandlerWithOptions(apps ApplicationRepository, codes AuthorizationCodeRepository, tokens OAuthTokenRepository, opts OAuthHandlerOptions) *OAuthHandler {
	h := &OAuthHandler{
		apps:        apps,
		codes:       codes,
		tokens:      tokens,
		accessTTL:   DefaultAccessTokenTTL,
		refreshTTL:  DefaultRefreshTokenTTL,
		authCodeTTL: AuthCodeTTL,
		now:         time.Now,
		consentTmpl: template.Must(template.New("consent").Parse(consentPageTemplate)),
	}
	if opts.AccessTokenTTL > 0 {
		h.accessTTL = opts.AccessTokenTTL
	}
	if opts.RefreshTokenTTL > 0 {
		h.refreshTTL = opts.RefreshTokenTTL
	}
	if opts.AuthCodeTTL > 0 {
		h.authCodeTTL = opts.AuthCodeTTL
	}
	if opts.Now != nil {
		h.now = opts.Now
	}
	return h
}

// RegisterRoutes wires the four OAuth endpoints on a chi router. /oauth/*
// sits at the ROOT, not under /api/v2/, to match the OAuth spec conventions
// third-party clients expect ("the authorization endpoint").
func (h *OAuthHandler) RegisterRoutes(r chi.Router) {
	r.Get("/oauth/authorize", h.AuthorizeGET)
	r.Post("/oauth/authorize", h.AuthorizePOST)
	r.Post("/oauth/token", h.Token)
}

// consentPageTemplate renders a minimal consent screen. Kept as a single
// template (not split into files) so `go build` doesn't need an embed
// directive for a single ~1KB string.
const consentPageTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>Authorize {{.AppName}}</title>
  <style>
    body { font-family: -apple-system, sans-serif; max-width: 480px; margin: 40px auto; padding: 20px; }
    h1 { font-size: 1.25rem; }
    .scope { display: block; padding: 4px 0; color: #555; }
    button { padding: 8px 20px; margin-right: 8px; cursor: pointer; }
    .approve { background: #2563eb; color: white; border: none; }
    .deny { background: white; border: 1px solid #ccc; }
  </style>
</head>
<body>
  <h1>Authorize {{.AppName}}</h1>
  {{if .AppDescription}}<p>{{.AppDescription}}</p>{{end}}
  <p>The application is requesting the following permissions:</p>
  <div>
    {{range .Scopes}}<code class="scope">{{.}}</code>{{else}}<em>no scopes requested</em>{{end}}
  </div>
  <form method="POST" action="/oauth/authorize">
    <input type="hidden" name="client_id" value="{{.ClientID}}" />
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}" />
    <input type="hidden" name="scope" value="{{.ScopeRaw}}" />
    <input type="hidden" name="state" value="{{.State}}" />
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}" />
    <input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}" />
    <button class="approve" type="submit" name="decision" value="approve">Approve</button>
    <button class="deny" type="submit" name="decision" value="deny">Deny</button>
  </form>
</body>
</html>`

type consentTemplateData struct {
	AppName             string
	AppDescription      string
	ClientID            string
	RedirectURI         string
	ScopeRaw            string
	Scopes              []string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

// AuthorizeGET renders the consent screen. The user must already be
// authenticated (the auth middleware injects them); rendering the screen
// does not issue anything — approval is a separate POST.
func (h *OAuthHandler) AuthorizeGET(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params, apiErr := parseAuthorizeParams(q)
	if apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	app, err := h.apps.GetByClientID(r.Context(), params.ClientID)
	if err != nil {
		if errors.Is(err, ErrApplicationNotFound) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("UnknownClient", map[string]string{
				"client_id": params.ClientID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ApplicationLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if err := ValidateRedirectURI(app, params.RedirectURI); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRedirectURI", map[string]string{
			"redirect_uri": params.RedirectURI,
		}))
		return
	}

	data := consentTemplateData{
		AppName:             app.Name,
		AppDescription:      app.Description,
		ClientID:            params.ClientID,
		RedirectURI:         params.RedirectURI,
		ScopeRaw:            params.ScopeRaw,
		Scopes:              params.Scopes,
		State:               params.State,
		CodeChallenge:       params.CodeChallenge,
		CodeChallengeMethod: params.CodeChallengeMethod,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = h.consentTmpl.Execute(w, data)
}

// AuthorizePOST handles the approval submission. On approve, it mints an
// authorization code bound to the supplied PKCE challenge and redirects the
// browser to redirect_uri?code=...&state=.... On deny, it redirects with
// ?error=access_denied.
func (h *OAuthHandler) AuthorizePOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidForm", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	params, apiErr := parseAuthorizeParams(r.PostForm)
	if apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	app, err := h.apps.GetByClientID(r.Context(), params.ClientID)
	if err != nil {
		if errors.Is(err, ErrApplicationNotFound) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("UnknownClient", map[string]string{
				"client_id": params.ClientID,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("ApplicationLookupFailed", map[string]string{"reason": err.Error()}))
		return
	}
	if err := ValidateRedirectURI(app, params.RedirectURI); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRedirectURI", map[string]string{
			"redirect_uri": params.RedirectURI,
		}))
		return
	}

	u := auth.UserFromContext(r.Context())
	if u == nil {
		apierror.WriteJSON(w, apierror.NewUnauthorized("MissingAuthenticatedUser", nil))
		return
	}

	decision := r.PostForm.Get("decision")
	if decision != "approve" {
		redirectWithError(w, r, params.RedirectURI, "access_denied", params.State)
		return
	}

	codeStr, err := GenerateAuthorizationCode()
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CodeGenerationFailed", map[string]string{"reason": err.Error()}))
		return
	}

	now := h.now()
	code := &AuthorizationCode{
		Code:                codeStr,
		ClientID:            params.ClientID,
		UserID:              u.ID,
		RedirectURI:         params.RedirectURI,
		Scopes:              params.Scopes,
		CodeChallenge:       params.CodeChallenge,
		CodeChallengeMethod: params.CodeChallengeMethod,
		ExpiresAt:           now.Add(h.authCodeTTL),
	}
	if err := h.codes.Create(r.Context(), code); err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("CodePersistFailed", map[string]string{"reason": err.Error()}))
		return
	}

	// Redirect to the client with the code.
	redirectURL, err := url.Parse(params.RedirectURI)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRedirectURI", map[string]string{
			"redirect_uri": params.RedirectURI,
		}))
		return
	}
	q := redirectURL.Query()
	q.Set("code", codeStr)
	if params.State != "" {
		q.Set("state", params.State)
	}
	redirectURL.RawQuery = q.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// TokenResponse is the JSON shape returned from /oauth/token. The field
// names are fixed by RFC 6749 §4.1.4 / §5.1 — changing any will break every
// conforming OAuth client.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// Token handles POST /oauth/token. It dispatches on grant_type; only
// authorization_code and client_credentials are supported.
func (h *OAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	grant := r.PostForm.Get("grant_type")
	switch grant {
	case "authorization_code":
		h.tokenAuthorizationCode(w, r)
	case "client_credentials":
		h.tokenClientCredentials(w, r)
	default:
		writeOAuthTokenError(w, http.StatusBadRequest, "unsupported_grant_type",
			fmt.Sprintf("grant_type %q is not supported", grant))
	}
}

func (h *OAuthHandler) tokenAuthorizationCode(w http.ResponseWriter, r *http.Request) {
	codeStr := r.PostForm.Get("code")
	redirectURI := r.PostForm.Get("redirect_uri")
	clientID := r.PostForm.Get("client_id")
	verifier := r.PostForm.Get("code_verifier")

	if codeStr == "" || redirectURI == "" || clientID == "" || verifier == "" {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_request",
			"code, redirect_uri, client_id and code_verifier are required")
		return
	}

	code, err := h.codes.GetByCode(r.Context(), codeStr)
	if err != nil {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_grant", "unknown authorization code")
		return
	}
	if err := code.IsUsable(h.now()); err != nil {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}
	if code.ClientID != clientID {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_grant", "code was issued to a different client")
		return
	}
	if code.RedirectURI != redirectURI {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri mismatch")
		return
	}
	if err := VerifyPKCE(code.CodeChallenge, verifier, code.CodeChallengeMethod); err != nil {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	// Mark consumed before issuing the token so a double-exchange fails.
	if err := h.codes.MarkConsumed(r.Context(), code.ID, h.now()); err != nil {
		writeOAuthTokenError(w, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	}

	resp, err := h.issueAccessAndRefresh(r, clientID, code.UserID, code.Scopes)
	if err != nil {
		writeOAuthTokenError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	writeTokenResponse(w, resp)
}

func (h *OAuthHandler) tokenClientCredentials(w http.ResponseWriter, r *http.Request) {
	clientID, clientSecret, err := extractClientCredentials(r)
	if err != nil {
		writeOAuthTokenError(w, http.StatusUnauthorized, "invalid_client", err.Error())
		return
	}

	app, err := h.apps.GetByClientID(r.Context(), clientID)
	if err != nil {
		writeOAuthTokenError(w, http.StatusUnauthorized, "invalid_client", "unknown client_id")
		return
	}

	if err := ValidateClientSecretShape(clientSecret); err != nil {
		writeOAuthTokenError(w, http.StatusUnauthorized, "invalid_client", "malformed client_secret")
		return
	}
	candidate := HashClientSecret(clientSecret)
	if subtle.ConstantTimeCompare(candidate, app.ClientSecretHash) != 1 {
		writeOAuthTokenError(w, http.StatusUnauthorized, "invalid_client", "client_secret mismatch")
		return
	}

	// Scope narrowing: client may request a subset of the app's registered
	// scopes; an empty request defaults to the full set.
	scopes := app.Scopes
	if raw := r.PostForm.Get("scope"); raw != "" {
		requested := strings.Fields(raw)
		scopes = intersectStringSlice(app.Scopes, requested)
	}

	// client_credentials is an app-level grant: no end-user identity.
	resp, err := h.issueAccessOnly(r, clientID, "", scopes)
	if err != nil {
		writeOAuthTokenError(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	writeTokenResponse(w, resp)
}

func (h *OAuthHandler) issueAccessAndRefresh(r *http.Request, clientID, userID string, scopes []string) (*TokenResponse, error) {
	accessRaw, accessPrefix, err := GenerateAccessToken()
	if err != nil {
		return nil, err
	}
	refreshRaw, refreshPrefix, err := GenerateRefreshToken()
	if err != nil {
		return nil, err
	}
	now := h.now()
	access := &OAuthToken{
		TokenHash:   HashOAuthToken(accessRaw),
		TokenPrefix: accessPrefix,
		TokenType:   TokenTypeAccess,
		ClientID:    clientID,
		UserID:      userID,
		Scopes:      scopes,
		ExpiresAt:   now.Add(h.accessTTL),
	}
	refresh := &OAuthToken{
		TokenHash:   HashOAuthToken(refreshRaw),
		TokenPrefix: refreshPrefix,
		TokenType:   TokenTypeRefresh,
		ClientID:    clientID,
		UserID:      userID,
		Scopes:      scopes,
		ExpiresAt:   now.Add(h.refreshTTL),
	}
	if err := h.tokens.Create(r.Context(), access); err != nil {
		return nil, err
	}
	if err := h.tokens.Create(r.Context(), refresh); err != nil {
		return nil, err
	}
	return &TokenResponse{
		AccessToken:  accessRaw,
		TokenType:    "Bearer",
		ExpiresIn:    int64(h.accessTTL.Seconds()),
		RefreshToken: refreshRaw,
		Scope:        strings.Join(scopes, " "),
	}, nil
}

func (h *OAuthHandler) issueAccessOnly(r *http.Request, clientID, userID string, scopes []string) (*TokenResponse, error) {
	accessRaw, accessPrefix, err := GenerateAccessToken()
	if err != nil {
		return nil, err
	}
	now := h.now()
	access := &OAuthToken{
		TokenHash:   HashOAuthToken(accessRaw),
		TokenPrefix: accessPrefix,
		TokenType:   TokenTypeAccess,
		ClientID:    clientID,
		UserID:      userID,
		Scopes:      scopes,
		ExpiresAt:   now.Add(h.accessTTL),
	}
	if err := h.tokens.Create(r.Context(), access); err != nil {
		return nil, err
	}
	return &TokenResponse{
		AccessToken: accessRaw,
		TokenType:   "Bearer",
		ExpiresIn:   int64(h.accessTTL.Seconds()),
		Scope:       strings.Join(scopes, " "),
	}, nil
}

// ------------------------------ helpers ------------------------------

type authorizeParams struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	ScopeRaw            string
	Scopes              []string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
}

// parseAuthorizeParams extracts the OAuth authorize query / form params and
// runs the spec-mandated shape checks. It does NOT validate the client_id
// or redirect_uri against the DB — callers do that next.
func parseAuthorizeParams(v url.Values) (*authorizeParams, *apierror.APIError) {
	p := &authorizeParams{
		ClientID:            v.Get("client_id"),
		RedirectURI:         v.Get("redirect_uri"),
		ResponseType:        v.Get("response_type"),
		ScopeRaw:            v.Get("scope"),
		State:               v.Get("state"),
		CodeChallenge:       v.Get("code_challenge"),
		CodeChallengeMethod: v.Get("code_challenge_method"),
	}
	if p.ClientID == "" {
		return nil, apierror.NewInvalidParameter("MissingClientID", map[string]string{"reason": "client_id is required"})
	}
	if p.RedirectURI == "" {
		return nil, apierror.NewInvalidParameter("MissingRedirectURI", map[string]string{"reason": "redirect_uri is required"})
	}
	if p.ResponseType != "" && p.ResponseType != "code" {
		return nil, apierror.NewInvalidParameter("UnsupportedResponseType", map[string]string{
			"response_type": p.ResponseType,
		})
	}
	if p.CodeChallenge == "" {
		return nil, apierror.NewInvalidParameter("MissingCodeChallenge", map[string]string{
			"reason": "PKCE is required; supply code_challenge",
		})
	}
	if p.CodeChallengeMethod == "" {
		p.CodeChallengeMethod = PKCEMethodS256
	}
	if p.CodeChallengeMethod != PKCEMethodS256 {
		return nil, apierror.NewInvalidParameter("UnsupportedCodeChallengeMethod", map[string]string{
			"code_challenge_method": p.CodeChallengeMethod,
		})
	}
	if p.ScopeRaw != "" {
		p.Scopes = strings.Fields(p.ScopeRaw)
	}
	return p, nil
}

// extractClientCredentials reads client_id + client_secret either from
// HTTP Basic Authorization (RFC 6749 §2.3.1) or from the request body
// (§2.3.1 fallback). Body takes priority only if Basic is absent.
func extractClientCredentials(r *http.Request) (string, string, error) {
	if user, pass, ok := r.BasicAuth(); ok {
		if user == "" {
			return "", "", errors.New("basic auth missing username")
		}
		return user, pass, nil
	}
	user := r.PostForm.Get("client_id")
	pass := r.PostForm.Get("client_secret")
	if user == "" || pass == "" {
		return "", "", errors.New("client_id and client_secret are required")
	}
	return user, pass, nil
}

// oauthTokenError is the RFC 6749 §5.2 error envelope — `error` /
// `error_description` — distinct from the regular apierror shape.
type oauthTokenError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

func writeOAuthTokenError(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(oauthTokenError{Error: code, ErrorDescription: description})
}

func writeTokenResponse(w http.ResponseWriter, resp *TokenResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func redirectWithError(w http.ResponseWriter, r *http.Request, redirectURI, errCode, state string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRedirectURI", map[string]string{
			"redirect_uri": redirectURI,
		}))
		return
	}
	q := u.Query()
	q.Set("error", errCode)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusFound)
}

// intersectStringSlice returns elements of `requested` that are also in
// `allowed`, preserving the order in `requested` and skipping duplicates.
func intersectStringSlice(allowed, requested []string) []string {
	allowSet := make(map[string]struct{}, len(allowed))
	for _, s := range allowed {
		allowSet[s] = struct{}{}
	}
	seen := make(map[string]struct{}, len(requested))
	var out []string
	for _, s := range requested {
		if _, ok := allowSet[s]; !ok {
			continue
		}
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
