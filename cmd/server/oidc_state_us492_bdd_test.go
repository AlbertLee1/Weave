//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
)

// US-492 — OIDC state HMAC + refresh token 轮换 (BDD).
//
// PRD 验收：
//   - state = HMAC(secret, nonce|timestamp); 回调验签 + 5min 时间窗
//   - grant_type=refresh_token 每次旋转 refresh token
//   - 负向测试：篡改 state 拒绝、过期 state 拒绝
//
// 走完整 wire surface:
//   - 真 testcontainers PG (refresh_tokens 表来自 migrations)
//   - 真 chi router + OIDCHandler.RegisterRoutes + RefreshHandler
//   - 真 oidctest.Server stand-in IdP + 真 coreos/go-oidc Verifier
//   - PG-backed RefreshStore（不是 in-memory）：rotation 行为可被 raw SQL 校验
//
// Scenario A — PRD positive (signed state → callback → rotated refresh):
//   Given /oidc/login mints an HMAC-signed state cookie + redirect
//   When the user-agent posts back with that signed state to /oidc/callback
//   Then the response is 200 with a refresh_token; PG refresh_tokens has one
//        un-revoked row for the user.
//   And  reusing the refresh_token at /api/auth/refresh mints a SECOND refresh
//        token (different bytes), revokes the first (raw SQL: revoked_at set),
//        and replaying the first now returns 401 (reuse-detected → chain burnt).
//
// Scenario B — PRD negative (tampered state rejected):
//   Given a callback whose state has one byte flipped after signing
//   When it is posted to /oidc/callback (cookie value matches the tampered state)
//   Then the response is 401 OIDCStateInvalid and PG refresh_tokens has
//        ZERO rows for the IdP-claimed user (raw SQL count = 0; no side effect).
//
// Scenario C — PRD negative (expired state rejected):
//   Given a state signed 6 minutes ago (outside the 5-minute window)
//   When it is posted to /oidc/callback
//   Then the response is 401 OIDCStateExpired and PG refresh_tokens still
//        has ZERO rows for that user (raw SQL count = 0; clock-skew abuse blocked).

type us492OIDCFixture struct {
	router       *chi.Mux
	pg           *testutil.PGContainer
	idpServer    *httptest.Server
	idpPrivate   *rsa.PrivateKey
	idpKeyID     string
	exchanger    *us492StubExchanger
	stateSigner  *auth.HMACStateSigner
	refreshSvc   *auth.RefreshService
	userEmail    string
	userSub      string
	verifierBind func() // forces verifier rebind if the IdP iss URL changes
}

// us492StubExchanger lets us control which id_token the OIDC handler verifies
// for a given authorization code, decoupling the test from a real OAuth2 token
// endpoint. It mimics the stubExchanger pattern from oidc_handler_test.go but
// lives in cmd/server to keep the BDD self-contained.
type us492StubExchanger struct {
	idToken  string
	lastCode string
}

func (s *us492StubExchanger) AuthCodeURL(state string, _ ...oauth2.AuthCodeOption) string {
	return "https://idp.example.com/authorize?state=" + state
}

func (s *us492StubExchanger) Exchange(_ context.Context, code string, _ ...oauth2.AuthCodeOption) (*oauth2.Token, error) {
	s.lastCode = code
	tok := (&oauth2.Token{AccessToken: "upstream-access", TokenType: "Bearer"}).
		WithExtra(map[string]interface{}{"id_token": s.idToken})
	return tok, nil
}

func setupUS492BDDFixture(t *testing.T) *us492OIDCFixture {
	t.Helper()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("RunMigrationsUp: %v", err)
	}

	// IdP stub with a real signed id_token so the coreos/go-oidc Verifier
	// exercises its real signature + claim validation path.
	idpPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("idp rsa: %v", err)
	}
	idpServer := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{{
			PublicKey: idpPriv.Public(),
			KeyID:     "us492-idp-key",
			Algorithm: oidc.RS256,
		}},
	}
	idpHTTP := httptest.NewServer(idpServer)
	t.Cleanup(idpHTTP.Close)
	idpServer.SetIssuer(idpHTTP.URL)

	userEmail := "alice@us492.example.com"
	userSub := "alice-us492"
	claims := map[string]interface{}{
		"iss":            idpHTTP.URL,
		"aud":            "weave-us492",
		"exp":            time.Now().Add(5 * time.Minute).Unix(),
		"iat":            time.Now().Unix(),
		"sub":            userSub,
		"email":          userEmail,
		"email_verified": true,
		"name":           "Alice US492",
	}
	rawClaims, _ := json.Marshal(claims)
	rawIDToken := oidctest.SignIDToken(idpPriv, "us492-idp-key", oidc.RS256, string(rawClaims))

	provider, err := oidc.NewProvider(context.Background(), idpHTTP.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: "weave-us492"})

	// JWT signer for the session-mint half of the callback.
	sessPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("session rsa: %v", err)
	}
	jwtSigner, err := auth.NewJWTSigner(sessPriv, &sessPriv.PublicKey, auth.JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}

	// PG-backed refresh store → rotation behaviour is auditable via raw SQL.
	refreshStore := auth.NewPGRefreshStore(pg.Pool)
	refreshSvc := auth.NewRefreshService(refreshStore, auth.RefreshServiceOptions{
		AbsoluteTTL: 7 * 24 * time.Hour,
	})

	// PG-backed user repo (refresh handler resolves the user by ID after rotation).
	userRepo := auth.NewPGUserRepository(pg.Pool)
	resolver := auth.NewRoleResolver(userRepo, time.Minute)

	exchanger := &us492StubExchanger{idToken: rawIDToken}

	stateSecret := make([]byte, 32)
	if _, err := rand.Read(stateSecret); err != nil {
		t.Fatalf("state secret: %v", err)
	}
	stateSigner, err := auth.NewHMACStateSigner(stateSecret, auth.DefaultStateTTL)
	if err != nil {
		t.Fatalf("NewHMACStateSigner: %v", err)
	}

	oidcHandler := auth.NewOIDCHandler(auth.OIDCHandlerDeps{
		Config: auth.OIDCConfig{
			IssuerURL:    idpHTTP.URL,
			ClientID:     "weave-us492",
			ClientSecret: "secret",
			RedirectURL:  "https://weave.example.com/api/auth/oidc/callback",
		},
		Exchanger:      exchanger,
		Verifier:       verifier,
		Users:          userRepo,
		Resolver:       resolver,
		Signer:         jwtSigner,
		RefreshService: refreshSvc,
		StateSigner:    stateSigner,
	})

	refreshHandler := auth.NewRefreshHandler(auth.RefreshHandlerDeps{
		Users:          userRepo,
		Resolver:       resolver,
		Signer:         jwtSigner,
		RefreshService: refreshSvc,
	})

	router := chi.NewRouter()
	oidcHandler.RegisterRoutes(router)
	router.Method(http.MethodPost, "/api/auth/refresh", refreshHandler)

	return &us492OIDCFixture{
		router:      router,
		pg:          pg,
		idpServer:   idpHTTP,
		idpPrivate:  idpPriv,
		idpKeyID:    "us492-idp-key",
		exchanger:   exchanger,
		stateSigner: stateSigner,
		refreshSvc:  refreshSvc,
		userEmail:   userEmail,
		userSub:     userSub,
	}
}

// countRefreshTokensBDD reads refresh_tokens directly so the assertion
// can't be mocked away by an in-memory store impl.
func countRefreshTokensBDD(t *testing.T, pg *testutil.PGContainer, userID string) int64 {
	t.Helper()
	var n int64
	err := pg.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1`, userID).Scan(&n)
	if err != nil {
		t.Fatalf("count refresh_tokens: %v", err)
	}
	return n
}

func countRevokedRefreshTokensBDD(t *testing.T, pg *testutil.PGContainer, userID string) int64 {
	t.Helper()
	var n int64
	err := pg.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM refresh_tokens WHERE user_id = $1 AND revoked_at IS NOT NULL`, userID).Scan(&n)
	if err != nil {
		t.Fatalf("count revoked refresh_tokens: %v", err)
	}
	return n
}

func TestBDD_US492_Given_SignedState_When_CallbackRotatesRefresh_Then_OriginalReuseBurnsChain(t *testing.T) {
	f := setupUS492BDDFixture(t)
	userID := "user:" + f.userEmail

	// --- Given: /oidc/login emits an HMAC-signed state in cookie + redirect.
	loginReq := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil)
	loginRec := httptest.NewRecorder()
	f.router.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusFound {
		t.Fatalf("login: got %d, want 302. body=%s", loginRec.Code, loginRec.Body.String())
	}
	var stateCookie string
	for _, c := range loginRec.Result().Cookies() {
		if c.Name == "weave_oidc_state" {
			stateCookie = c.Value
		}
	}
	if stateCookie == "" {
		t.Fatal("login: state cookie missing")
	}
	if err := f.stateSigner.Verify(stateCookie); err != nil {
		t.Fatalf("login state failed HMAC verify: %v", err)
	}

	// --- When: callback round-trips that signed state.
	cbReq := httptest.NewRequest(http.MethodGet,
		"/api/auth/oidc/callback?code=us492-auth-code&state="+stateCookie, nil)
	cbReq.AddCookie(&http.Cookie{Name: "weave_oidc_state", Value: stateCookie})
	cbRec := httptest.NewRecorder()
	f.router.ServeHTTP(cbRec, cbReq)
	if cbRec.Code != http.StatusOK {
		t.Fatalf("callback: got %d, want 200. body=%s", cbRec.Code, cbRec.Body.String())
	}
	var firstSession auth.LoginResponse
	if err := json.NewDecoder(cbRec.Body).Decode(&firstSession); err != nil {
		t.Fatalf("decode callback body: %v", err)
	}
	if firstSession.RefreshToken == "" {
		t.Fatal("callback: refresh_token missing")
	}

	// --- Then: PG holds exactly one un-revoked refresh row for our user.
	if n := countRefreshTokensBDD(t, f.pg, userID); n != 1 {
		t.Fatalf("post-callback refresh row count: got %d want 1", n)
	}
	if n := countRevokedRefreshTokensBDD(t, f.pg, userID); n != 0 {
		t.Fatalf("post-callback revoked refresh count: got %d want 0", n)
	}

	// --- And: a refresh-grant call rotates the token.
	rotated := postRefreshUS492(t, f, firstSession.RefreshToken)
	if rotated.RefreshToken == "" {
		t.Fatal("rotation: new refresh_token missing")
	}
	if rotated.RefreshToken == firstSession.RefreshToken {
		t.Fatal("rotation must mint a DIFFERENT refresh_token, not echo the input")
	}
	if n := countRefreshTokensBDD(t, f.pg, userID); n != 2 {
		t.Fatalf("post-rotation refresh row count: got %d want 2", n)
	}
	if n := countRevokedRefreshTokensBDD(t, f.pg, userID); n != 1 {
		t.Fatalf("post-rotation revoked refresh count: got %d want 1 (original must be revoked)", n)
	}

	// --- And: replaying the ORIGINAL refresh token burns the entire chain
	// (RFC 9700 reuse-detection — the PRD-mandated "rotate per refresh" half).
	replay := httptest.NewRequest(http.MethodPost, "/api/auth/refresh",
		bytes.NewReader(mustJSONUS492(t, map[string]string{
			"refresh_token": firstSession.RefreshToken,
		})))
	replay.Header.Set("Content-Type", "application/json")
	replayRec := httptest.NewRecorder()
	f.router.ServeHTTP(replayRec, replay)
	if replayRec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse-detection: got %d want 401. body=%s", replayRec.Code, replayRec.Body.String())
	}
	// After burn, every row for this user is revoked.
	total := countRefreshTokensBDD(t, f.pg, userID)
	revoked := countRevokedRefreshTokensBDD(t, f.pg, userID)
	if revoked != total {
		t.Fatalf("post-burn: revoked=%d total=%d (chain not fully revoked)", revoked, total)
	}
}

func TestBDD_US492_Given_TamperedState_When_CallbackPosted_Then_401AndNoSideEffect(t *testing.T) {
	f := setupUS492BDDFixture(t)
	userID := "user:" + f.userEmail

	good, err := f.stateSigner.Sign(time.Now())
	if err != nil {
		t.Fatalf("sign state: %v", err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(good)
	if err != nil {
		t.Fatalf("decode state: %v", err)
	}
	raw[0] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(raw)

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/oidc/callback?code=any&state="+tampered, nil)
	// Cookie matches the tampered state — proves HMAC verify fires BEFORE
	// the cookie comparison and rejects a payload-level forgery even when
	// the attacker controls both the URL and the cookie.
	req.AddCookie(&http.Cookie{Name: "weave_oidc_state", Value: tampered})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCStateInvalid") {
		t.Fatalf("body should name OIDCStateInvalid: %s", rec.Body.String())
	}
	if f.exchanger.lastCode != "" {
		t.Fatalf("Exchange must NOT be called on tampered state, got code=%q", f.exchanger.lastCode)
	}
	// Raw SQL: zero side effect — no refresh token, no user row should
	// have been minted before the HMAC gate rejected the request.
	if n := countRefreshTokensBDD(t, f.pg, userID); n != 0 {
		t.Fatalf("tampered path created %d refresh rows; want 0 (no side effect)", n)
	}
}

func TestBDD_US492_Given_ExpiredState_When_CallbackPosted_Then_401ExpiredAndNoSideEffect(t *testing.T) {
	f := setupUS492BDDFixture(t)
	userID := "user:" + f.userEmail

	issued := time.Now()
	state, err := f.stateSigner.Sign(issued)
	if err != nil {
		t.Fatalf("sign state: %v", err)
	}
	// Push the signer's clock past the 5-minute window.
	f.stateSigner.SetNow(func() time.Time { return issued.Add(6 * time.Minute) })

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/oidc/callback?code=any&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: "weave_oidc_state", Value: state})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCStateExpired") {
		t.Fatalf("body should name OIDCStateExpired: %s", rec.Body.String())
	}
	if f.exchanger.lastCode != "" {
		t.Fatalf("Exchange must NOT be called on expired state, got code=%q", f.exchanger.lastCode)
	}
	if n := countRefreshTokensBDD(t, f.pg, userID); n != 0 {
		t.Fatalf("expired path created %d refresh rows; want 0 (no side effect)", n)
	}
}

func postRefreshUS492(t *testing.T, f *us492OIDCFixture, plain string) auth.LoginResponse {
	t.Helper()
	body := mustJSONUS492(t, map[string]string{"refresh_token": plain})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp auth.LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode refresh body: %v", err)
	}
	return resp
}

func mustJSONUS492(t *testing.T, v any) []byte {
	t.Helper()
	bs, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return bs
}
