// US-492 — handler-level acceptance for HMAC-signed OIDC state.
//
// The OIDCHandler must reject any callback whose state is either
//   - tampered (HMAC mismatch / decoded length wrong / garbled base64), or
//   - older than the 5-minute window (HMAC valid but stale).
//
// Both classes surface as 401 with a distinct apierror name so SDKs / SIEM
// rules can split "we were attacked" from "the user took too long".
package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/coreos/go-oidc/v3/oidc/oidctest"
)

// newSignedStateHarness wires a fresh oidctest.Server + a SHARED HMAC state
// signer and exposes both so tests can mint authentic states or substitute
// the signer's clock to age them out.
func newSignedStateHarness(t *testing.T, claims map[string]interface{}) (*OIDCHandler, *stubExchanger, *HMACStateSigner) {
	t.Helper()

	providerPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	oidcTestSrv := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{{
			PublicKey: providerPriv.Public(),
			KeyID:     "test-key-id",
			Algorithm: oidc.RS256,
		}},
	}
	httpSrv := httptest.NewServer(oidcTestSrv)
	t.Cleanup(httpSrv.Close)
	oidcTestSrv.SetIssuer(httpSrv.URL)

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

	provider, err := oidc.NewProvider(t.Context(), httpSrv.URL)
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
	return h, exchanger, stateSigner
}

// TestUS492_Login_EmitsHMACSignedState mints a /login redirect and proves the
// signed state value parses + verifies against the same signer the handler
// holds. A pre-US-492 random-base64 state would fail Verify.
func TestUS492_Login_EmitsHMACSignedState(t *testing.T) {
	h, _, signer := newSignedStateHarness(t, goodClaims())

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil)
	rec := httptest.NewRecorder()
	h.Login(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("login redirect: got %d, want 302", rec.Code)
	}
	loc := rec.Header().Get("Location")
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("parse loc: %v", err)
	}
	state := u.Query().Get("state")
	if state == "" {
		t.Fatal("state missing")
	}
	if err := signer.Verify(state); err != nil {
		t.Fatalf("login state should verify: %v", err)
	}
	// Cookie must equal the signed state so the cookie-binding CSRF defense
	// continues to fire after HMAC verify.
	var cookieValue string
	for _, c := range rec.Result().Cookies() {
		if c.Name == stateCookieName {
			cookieValue = c.Value
		}
	}
	if cookieValue != state {
		t.Fatalf("cookie value %q != state %q", cookieValue, state)
	}
}

// TestUS492_Callback_RejectsTamperedState validates the PRD negative: a state
// whose payload byte was flipped — even though the cookie matches it exactly
// — must be rejected as 401 OIDCStateInvalid before the exchange path runs.
func TestUS492_Callback_RejectsTamperedState(t *testing.T) {
	h, exchanger, signer := newSignedStateHarness(t, goodClaims())

	good, err := signer.Sign(time.Now())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(good)
	raw[0] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(raw)

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/oidc/callback?code=abc&state="+tampered, nil)
	// Cookie deliberately matches the tampered state to prove HMAC verify
	// fires BEFORE cookie comparison.
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: tampered})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCStateInvalid") {
		t.Fatalf("body should name OIDCStateInvalid: %s", rec.Body.String())
	}
	if exchanger.lastCode != "" {
		t.Fatalf("Exchange must NOT be called on tampered state, got code=%q", exchanger.lastCode)
	}
}

// TestUS492_Callback_RejectsExpiredState ages the signer's clock past the
// 5-minute window so the HMAC still validates but the timestamp window
// rejects. PRD distinguishes Invalid from Expired so client UX can offer
// "session timed out, try again".
func TestUS492_Callback_RejectsExpiredState(t *testing.T) {
	h, exchanger, signer := newSignedStateHarness(t, goodClaims())

	issued := time.Now()
	state, err := signer.Sign(issued)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	signer.SetNow(func() time.Time { return issued.Add(6 * time.Minute) })

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/oidc/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCStateExpired") {
		t.Fatalf("body should name OIDCStateExpired: %s", rec.Body.String())
	}
	if exchanger.lastCode != "" {
		t.Fatalf("Exchange must NOT be called on expired state, got code=%q", exchanger.lastCode)
	}
}

// TestUS492_Callback_HappyPath_WithSignedState locks in that the new state
// signing path still completes a callback end-to-end and mints a refresh
// token (the rotated-on-each-refresh half of US-492 is covered by the
// /api/auth/refresh tests; this just guards no-regression on /oidc/callback).
func TestUS492_Callback_HappyPath_WithSignedState(t *testing.T) {
	h, _, signer := newSignedStateHarness(t, goodClaims())

	state, err := signer.Sign(time.Now())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/oidc/callback?code=abc&state="+state, nil)
	req.AddCookie(&http.Cookie{Name: stateCookieName, Value: state})
	rec := httptest.NewRecorder()
	h.Callback(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("happy path: got %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	var resp LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.RefreshToken == "" {
		t.Fatal("refresh token missing from happy-path response")
	}
}

// TestUS492_RefreshToken_RotatesEachCall covers the second half of the PRD:
// every successful /api/auth/refresh must mint a fresh refresh token and
// revoke the previous one. Replaying the original (now-revoked) refresh
// must fail with reuse-detection.
func TestUS492_RefreshToken_RotatesEachCall(t *testing.T) {
	ctx := t.Context()
	store := NewMemoryRefreshStore()
	rs := NewRefreshService(store, RefreshServiceOptions{AbsoluteTTL: time.Hour})

	original, originalRec, err := rs.Generate(ctx, "user:alice", "")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	rotated1, rec1, err := rs.Rotate(ctx, original)
	if err != nil {
		t.Fatalf("Rotate #1: %v", err)
	}
	if rotated1 == original {
		t.Fatal("Rotate must return a fresh plaintext, not echo the input")
	}
	if rec1.ParentID != originalRec.ID {
		t.Fatalf("rotated record parent=%q, want %q", rec1.ParentID, originalRec.ID)
	}

	rotated2, rec2, err := rs.Rotate(ctx, rotated1)
	if err != nil {
		t.Fatalf("Rotate #2: %v", err)
	}
	if rotated2 == rotated1 {
		t.Fatal("second rotation must mint a different token")
	}
	if rec2.ParentID != rec1.ID {
		t.Fatalf("rotated #2 parent=%q, want %q", rec2.ParentID, rec1.ID)
	}

	// Replay of the first (already-rotated) token must trigger reuse detection
	// and burn the entire chain — that's the PRD-mandated "rotation per
	// refresh" invariant: there can only ever be one live token per chain.
	if _, _, err := rs.Rotate(ctx, original); err != ErrRefreshTokenReuseDetected {
		t.Fatalf("replay of original: got %v, want ErrRefreshTokenReuseDetected", err)
	}
	if _, _, err := rs.Rotate(ctx, rotated2); err != ErrRefreshTokenReuseDetected {
		t.Fatalf("post-burn rotation: got %v, want ErrRefreshTokenReuseDetected", err)
	}
}
