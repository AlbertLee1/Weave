package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"golang.org/x/oauth2"
)

// stubExchanger substitutes for *oauth2.Config in tests so we don't need to
// spin up a real token endpoint. AuthCodeURL is a plain string formatter;
// Exchange returns a canned token with whatever id_token the harness seeded.
type stubExchanger struct {
	authURL   string
	idToken   string
	err       error
	lastCode  string
	lastState string
}

func (s *stubExchanger) AuthCodeURL(state string, _ ...oauth2.AuthCodeOption) string {
	s.lastState = state
	base := s.authURL
	if base == "" {
		base = "https://idp.example.com/authorize"
	}
	u, _ := url.Parse(base)
	q := u.Query()
	q.Set("state", state)
	q.Set("client_id", "test-client")
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *stubExchanger) Exchange(_ context.Context, code string, _ ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	s.lastCode = code
	if s.err != nil {
		return nil, s.err
	}
	tok := (&oauth2.Token{
		AccessToken: "upstream-access",
		TokenType:   "Bearer",
	}).WithExtra(map[string]interface{}{"id_token": s.idToken})
	return tok, nil
}

// newOIDCHarness stands up a handler wired against a real oidctest.Server
// (so the coreos/go-oidc v3 Verifier runs the actual signature + claim
// validation path). The Exchanger is stubbed — we don't need a real token
// endpoint because the id_token bytes are pre-signed inside the test.
func newOIDCHarness(t *testing.T, claims map[string]interface{}) (*OIDCHandler, *stubExchanger, *fakeUserRepo) {
	t.Helper()

	providerPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	oidcTestSrv := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{
			{
				PublicKey: providerPriv.Public(),
				KeyID:     "test-key-id",
				Algorithm: oidc.RS256,
			},
		},
	}
	httpSrv := httptest.NewServer(oidcTestSrv)
	t.Cleanup(httpSrv.Close)
	oidcTestSrv.SetIssuer(httpSrv.URL)

	// Fill in iss / exp / iat / aud if the caller didn't.
	if _, ok := claims["iss"]; !ok {
		claims["iss"] = httpSrv.URL
	}
	if _, ok := claims["aud"]; !ok {
		claims["aud"] = "test-client"
	}
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(5 * time.Minute).Unix()
	}
	if _, ok := claims["iat"]; !ok {
		claims["iat"] = time.Now().Unix()
	}
	rawClaims, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	rawIDToken := oidctest.SignIDToken(providerPriv, "test-key-id", oidc.RS256, string(rawClaims))

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, httpSrv.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: "test-client"})

	repo := newFakeUserRepo()
	resolver := NewRoleResolver(repo, time.Minute)

	sessPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewJWTSigner(sessPriv, &sessPriv.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	refreshStore := NewMemoryRefreshStore()
	rs := NewRefreshService(refreshStore, RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})

	exchanger := &stubExchanger{
		authURL: httpSrv.URL + "/authorize",
		idToken: rawIDToken,
	}

	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	stateSigner, err := NewHMACStateSigner(secret, DefaultStateTTL)
	if err != nil {
		t.Fatal(err)
	}
	h := NewOIDCHandler(OIDCHandlerDeps{
		Config: OIDCConfig{
			IssuerURL:    httpSrv.URL,
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			RedirectURL:  "https://weave.example.com/api/auth/oidc/callback",
		},
		Exchanger:      exchanger,
		Verifier:       verifier,
		Users:          repo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
		StateSigner:    stateSigner,
	})
	return h, exchanger, repo
}

// signValidState mints an HMAC-signed state that the harness's signer will
// accept. Existing pre-US-492 tests called this implicitly via a hardcoded
// random string; now they must round-trip through the real signer.
func (h *OIDCHandler) signValidState(t *testing.T) string {
	t.Helper()
	state, err := h.deps.StateSigner.Sign(h.deps.Now())
	if err != nil {
		t.Fatalf("sign state: %v", err)
	}
	return state
}

func goodClaims() map[string]interface{} {
	return map[string]interface{}{
		"sub":            "alice-123",
		"email":          "alice@example.com",
		"email_verified": true,
		"name":           "Alice Example",
	}
}

func TestOIDCHandler_Login_SetsStateCookieAndRedirects(t *testing.T) {
	h, exchanger, _ := newOIDCHarness(t, goodClaims())

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("got %d, want 302. body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if loc == "" {
		t.Fatal("no Location header on redirect")
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse loc: %v", err)
	}
	if state := u.Query().Get("state"); state == "" {
		t.Fatal("state missing in redirect URL")
	} else if state != exchanger.lastState {
		t.Fatalf("redirect state=%q doesn't match exchanger state=%q", state, exchanger.lastState)
	}

	var found *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == stateCookieName {
			found = c
		}
	}
	if found == nil {
		t.Fatal("state cookie not set")
	}
	if !found.HttpOnly {
		t.Fatal("state cookie must be HttpOnly")
	}
	if found.Value != exchanger.lastState {
		t.Fatalf("cookie value %q != AuthCodeURL state %q", found.Value, exchanger.lastState)
	}
}

func TestOIDCHandler_Login_RejectsNonGET(t *testing.T) {
	h, _, _ := newOIDCHarness(t, goodClaims())

	req := httptest.NewRequest(http.MethodPost, "/api/auth/oidc/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code == http.StatusFound {
		t.Fatal("POST should not redirect")
	}
}

func TestOIDCHandler_Callback_HappyPath(t *testing.T) {
	h, exchanger, repo := newOIDCHarness(t, goodClaims())

	state := h.signValidState(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	if exchanger.lastCode != "abc" {
		t.Fatalf("Exchange called with code=%q, want %q", exchanger.lastCode, "abc")
	}
	var resp LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("empty tokens in response")
	}
	if resp.TokenType != "Bearer" {
		t.Fatalf("token_type=%q, want Bearer", resp.TokenType)
	}
	if resp.User.Email != "alice@example.com" {
		t.Fatalf("user.email=%q", resp.User.Email)
	}
	if resp.User.ID != "user:alice@example.com" {
		t.Fatalf("user.id=%q", resp.User.ID)
	}
	if resp.User.Name != "Alice Example" {
		t.Fatalf("user.name=%q", resp.User.Name)
	}
	if _, err := repo.GetUserByEmail(context.Background(), "alice@example.com"); err != nil {
		t.Fatalf("user not persisted: %v", err)
	}
}

func TestOIDCHandler_Callback_RejectsStateMismatch(t *testing.T) {
	h, _, _ := newOIDCHarness(t, goodClaims())

	// Both states are HMAC-valid (so we get past US-492's HMAC gate) but the
	// cookie value differs from the query — the legacy CSRF cookie-binding
	// defense must still trip in that case.
	queryState := h.signValidState(t)
	cookieState := h.signValidState(t)
	if queryState == cookieState {
		t.Fatal("nonces collided — test setup broken")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+queryState, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: cookieState})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OIDCStateMismatch") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOIDCHandler_Callback_MissingStateCookie(t *testing.T) {
	h, _, _ := newOIDCHarness(t, goodClaims())

	// Use an HMAC-valid state so the request gets past the US-492 verify
	// gate; with no cookie at all, the cookie-binding defense must still
	// reject the callback.
	state := h.signValidState(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+state, nil)
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}

func TestOIDCHandler_Callback_MissingCode(t *testing.T) {
	h, _, _ := newOIDCHarness(t, goodClaims())

	state := h.signValidState(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCMissingCode") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOIDCHandler_Callback_SurfacesProviderError(t *testing.T) {
	h, _, _ := newOIDCHarness(t, goodClaims())

	state := h.signValidState(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?error=access_denied&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "OIDCProviderError") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOIDCHandler_Callback_ExchangeFailure(t *testing.T) {
	h, exch, _ := newOIDCHarness(t, goodClaims())
	exch.err = errors.New("token endpoint unreachable")

	state := h.signValidState(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCTokenExchangeFailed") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOIDCHandler_Callback_InvalidIDToken(t *testing.T) {
	// Build a claim set whose iss doesn't match the verifier's expected
	// issuer — Verify should reject it.
	badClaims := goodClaims()
	badClaims["iss"] = "https://wrong-issuer.example.com"
	h, _, _ := newOIDCHarness(t, badClaims)

	state := h.signValidState(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCIDTokenInvalid") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOIDCHandler_Callback_RejectsMissingEmailClaim(t *testing.T) {
	claims := goodClaims()
	delete(claims, "email")
	h, _, _ := newOIDCHarness(t, claims)

	state := h.signValidState(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCClaimsIncomplete") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOIDCHandler_Callback_ExistingUserKeepsPassword(t *testing.T) {
	h, _, repo := newOIDCHarness(t, goodClaims())

	// Seed an existing UserRecord with a pre-set password — OIDC path must
	// NOT overwrite password_hash.
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Old Name", "editor")

	state := h.signValidState(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	u, err := repo.GetUserByEmail(context.Background(), "alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if u.PasswordHash == "" {
		t.Fatal("password hash wiped by OIDC login")
	}
	if u.ID != "user:alice@example.com" {
		t.Fatalf("user.id mutated to %q", u.ID)
	}
}

func TestOIDCHandler_Callback_SuccessRedirectURL(t *testing.T) {
	h, _, _ := newOIDCHarness(t, goodClaims())
	h.deps.Config.SuccessRedirectURL = "https://weave.example.com/sso-done"

	state := h.signValidState(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("got %d, want 302. body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://weave.example.com/sso-done?access_token=") {
		t.Fatalf("unexpected redirect %q", loc)
	}
	if !strings.Contains(loc, "refresh_token=") {
		t.Fatalf("refresh_token missing from redirect %q", loc)
	}
}
