package developer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/auth"
)

// oauthHarness bundles the fake repos + a handler so test bodies stay
// readable. Tests poke at the repo fields directly to seed state.
type oauthHarness struct {
	apps   *fakeApplicationRepo
	codes  *fakeAuthCodeRepo
	tokens *fakeOAuthTokenRepo
	h      *OAuthHandler
	app    *Application
	secret string
	now    time.Time
}

func newOAuthHarness(t *testing.T) *oauthHarness {
	t.Helper()
	apps := newFakeApplicationRepo()
	codes := newFakeAuthCodeRepo()
	tokens := newFakeOAuthTokenRepo()

	cid, _ := GenerateClientID()
	sec, _ := GenerateClientSecret()
	app := &Application{
		Name:             "TestApp",
		Description:      "example client",
		ClientID:         cid,
		ClientSecretHash: HashClientSecret(sec),
		RedirectURIs:     []string{"https://client.example.com/cb"},
		Scopes:           []string{"read:objects", "write:objects"},
		CreatedBy:        "user:alice@example.com",
	}
	if err := apps.Create(nil, app); err != nil {
		t.Fatalf("seed app: %v", err)
	}
	frozen := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	h := NewOAuthHandlerWithOptions(apps, codes, tokens, OAuthHandlerOptions{
		Now: func() time.Time { return frozen },
	})
	return &oauthHarness{apps: apps, codes: codes, tokens: tokens, h: h, app: app, secret: sec, now: frozen}
}

func TestAuthorizeGET_RendersConsentPage(t *testing.T) {
	h := newOAuthHarness(t)
	verifier := strings.Repeat("a", 43)
	challenge := ComputePKCEChallenge(verifier)
	q := url.Values{}
	q.Set("client_id", h.app.ClientID)
	q.Set("redirect_uri", "https://client.example.com/cb")
	q.Set("response_type", "code")
	q.Set("scope", "read:objects")
	q.Set("state", "xyz")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	req = req.WithContext(auth.WithUser(req.Context(), &auth.User{ID: "user:alice@example.com"}))
	rec := httptest.NewRecorder()
	h.h.AuthorizeGET(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type: %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "TestApp") {
		t.Errorf("consent page missing app name: %s", body)
	}
	if !strings.Contains(body, "read:objects") {
		t.Errorf("consent page missing scope: %s", body)
	}
}

func TestAuthorizeGET_RejectsUnknownClient(t *testing.T) {
	h := newOAuthHarness(t)
	q := url.Values{}
	q.Set("client_id", "wapp_UNKNOWNCLIENT1234567890")
	q.Set("redirect_uri", "https://client.example.com/cb")
	q.Set("response_type", "code")
	q.Set("code_challenge", ComputePKCEChallenge(strings.Repeat("a", 43)))

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.h.AuthorizeGET(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAuthorizeGET_RejectsMismatchedRedirectURI(t *testing.T) {
	h := newOAuthHarness(t)
	q := url.Values{}
	q.Set("client_id", h.app.ClientID)
	q.Set("redirect_uri", "https://evil.example.com/cb") // not registered
	q.Set("response_type", "code")
	q.Set("code_challenge", ComputePKCEChallenge(strings.Repeat("a", 43)))

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.h.AuthorizeGET(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAuthorizeGET_RejectsMissingPKCEChallenge(t *testing.T) {
	h := newOAuthHarness(t)
	q := url.Values{}
	q.Set("client_id", h.app.ClientID)
	q.Set("redirect_uri", "https://client.example.com/cb")
	q.Set("response_type", "code")
	// no code_challenge

	req := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+q.Encode(), nil)
	rec := httptest.NewRecorder()
	h.h.AuthorizeGET(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestAuthorizePOST_ApproveRedirectsWithCode(t *testing.T) {
	h := newOAuthHarness(t)
	verifier := strings.Repeat("a", 43)
	challenge := ComputePKCEChallenge(verifier)

	form := url.Values{}
	form.Set("client_id", h.app.ClientID)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("response_type", "code")
	form.Set("scope", "read:objects")
	form.Set("state", "xyz")
	form.Set("code_challenge", challenge)
	form.Set("code_challenge_method", "S256")
	form.Set("decision", "approve")

	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.User{ID: "user:alice@example.com"}))
	rec := httptest.NewRecorder()
	h.h.AuthorizePOST(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d body=%s", rec.Code, rec.Body.String())
	}
	loc, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("bad redirect location: %v", err)
	}
	if loc.Host != "client.example.com" {
		t.Errorf("redirect host: got %q", loc.Host)
	}
	code := loc.Query().Get("code")
	if !strings.HasPrefix(code, AuthCodePrefix) {
		t.Errorf("redirect code missing prefix: %q", code)
	}
	if loc.Query().Get("state") != "xyz" {
		t.Errorf("state not round-tripped: %q", loc.Query().Get("state"))
	}

	// The code should exist in the repo with the supplied challenge.
	stored, err := h.codes.GetByCode(req.Context(), code)
	if err != nil {
		t.Fatalf("GetByCode: %v", err)
	}
	if stored.CodeChallenge != challenge {
		t.Errorf("stored challenge mismatch")
	}
	if stored.UserID != "user:alice@example.com" {
		t.Errorf("stored user: %q", stored.UserID)
	}
}

func TestAuthorizePOST_DenyRedirectsWithError(t *testing.T) {
	h := newOAuthHarness(t)
	form := url.Values{}
	form.Set("client_id", h.app.ClientID)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("response_type", "code")
	form.Set("state", "abc")
	form.Set("code_challenge", ComputePKCEChallenge(strings.Repeat("a", 43)))
	form.Set("code_challenge_method", "S256")
	form.Set("decision", "deny")

	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.User{ID: "user:alice@example.com"}))
	rec := httptest.NewRecorder()
	h.h.AuthorizePOST(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Query().Get("error") != "access_denied" {
		t.Errorf("expected error=access_denied, got %q", loc.Query().Get("error"))
	}
	if loc.Query().Get("state") != "abc" {
		t.Errorf("state not round-tripped on deny")
	}
}

// issueCodeFor drives the authorize POST end-to-end and returns the fresh
// authorization code. Used by the token-exchange tests below so they don't
// have to reconstruct the challenge/verifier plumbing each time.
func issueCodeFor(t *testing.T, h *oauthHarness, scopes, verifier string) string {
	t.Helper()
	challenge := ComputePKCEChallenge(verifier)
	form := url.Values{}
	form.Set("client_id", h.app.ClientID)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("response_type", "code")
	if scopes != "" {
		form.Set("scope", scopes)
	}
	form.Set("code_challenge", challenge)
	form.Set("code_challenge_method", "S256")
	form.Set("decision", "approve")

	req := httptest.NewRequest(http.MethodPost, "/oauth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(auth.WithUser(req.Context(), &auth.User{ID: "user:alice@example.com"}))
	rec := httptest.NewRecorder()
	h.h.AuthorizePOST(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize: %d %s", rec.Code, rec.Body.String())
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	return loc.Query().Get("code")
}

func TestToken_AuthorizationCodeFlow_ReturnsAccessAndRefresh(t *testing.T) {
	h := newOAuthHarness(t)
	verifier := strings.Repeat("a", 43)
	code := issueCodeFor(t, h, "read:objects", verifier)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("client_id", h.app.ClientID)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.h.Token(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp TokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.HasPrefix(resp.AccessToken, AccessTokenMarker) {
		t.Errorf("access_token prefix: %q", resp.AccessToken)
	}
	if !strings.HasPrefix(resp.RefreshToken, RefreshTokenMarker) {
		t.Errorf("refresh_token prefix: %q", resp.RefreshToken)
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type: %q", resp.TokenType)
	}
	if resp.Scope != "read:objects" {
		t.Errorf("scope: %q", resp.Scope)
	}
	if resp.ExpiresIn <= 0 {
		t.Errorf("expires_in: %d", resp.ExpiresIn)
	}
}

func TestToken_AuthorizationCodeIsSingleUse(t *testing.T) {
	h := newOAuthHarness(t)
	verifier := strings.Repeat("a", 43)
	code := issueCodeFor(t, h, "", verifier)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("client_id", h.app.ClientID)
	form.Set("code_verifier", verifier)

	// First redemption succeeds.
	req1 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req1.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec1 := httptest.NewRecorder()
	h.h.Token(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first redemption: %d %s", rec1.Code, rec1.Body.String())
	}

	// Second redemption MUST fail — single-use.
	req2 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	h.h.Token(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("second redemption: expected 400, got %d", rec2.Code)
	}
}

func TestToken_AuthorizationCodeRejectsWrongVerifier(t *testing.T) {
	h := newOAuthHarness(t)
	verifier := strings.Repeat("a", 43)
	code := issueCodeFor(t, h, "", verifier)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("client_id", h.app.ClientID)
	form.Set("code_verifier", strings.Repeat("b", 43)) // wrong

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.h.Token(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	var errResp oauthTokenError
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp.Error != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %q", errResp.Error)
	}
}

func TestToken_AuthorizationCodeRejectsRedirectURIMismatch(t *testing.T) {
	h := newOAuthHarness(t)
	verifier := strings.Repeat("a", 43)
	code := issueCodeFor(t, h, "", verifier)

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://other.example.com/cb") // differs from the bound URI
	form.Set("client_id", h.app.ClientID)
	form.Set("code_verifier", verifier)

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.h.Token(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestToken_ClientCredentialsFlow_ReturnsAccessOnly(t *testing.T) {
	h := newOAuthHarness(t)

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", h.app.ClientID)
	form.Set("client_secret", h.secret)
	form.Set("scope", "read:objects")

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.h.Token(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp TokenResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if !strings.HasPrefix(resp.AccessToken, AccessTokenMarker) {
		t.Errorf("access_token: %q", resp.AccessToken)
	}
	if resp.RefreshToken != "" {
		t.Errorf("client_credentials must NOT issue refresh token, got %q", resp.RefreshToken)
	}
	if resp.Scope != "read:objects" {
		t.Errorf("scope narrowing failed: %q", resp.Scope)
	}
}

func TestToken_ClientCredentialsRejectsBadSecret(t *testing.T) {
	h := newOAuthHarness(t)

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", h.app.ClientID)
	form.Set("client_secret", "wsec_DIFFERENT000000000000000000000000000000000000000000000")

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.h.Token(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestToken_RejectsUnsupportedGrantType(t *testing.T) {
	h := newOAuthHarness(t)
	form := url.Values{}
	form.Set("grant_type", "password")
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.h.Token(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
	var errResp oauthTokenError
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp.Error != "unsupported_grant_type" {
		t.Errorf("expected unsupported_grant_type, got %q", errResp.Error)
	}
}

// TestOAuthHandler_RegisterRoutes verifies the router wiring — chi.Walk
// sees the three endpoints with the right methods.
func TestOAuthHandler_RegisterRoutes(t *testing.T) {
	h := newOAuthHarness(t)
	r := chi.NewRouter()
	h.h.RegisterRoutes(r)
	expected := map[string]string{
		"/oauth/authorize": "GET,POST",
		"/oauth/token":     "POST",
	}
	found := map[string][]string{}
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		found[route] = append(found[route], method)
		return nil
	})
	for path, wantMethods := range expected {
		got := strings.Join(found[path], ",")
		for _, m := range strings.Split(wantMethods, ",") {
			if !strings.Contains(got, m) {
				t.Errorf("route %s missing method %s (have %s)", path, m, got)
			}
		}
	}
}
