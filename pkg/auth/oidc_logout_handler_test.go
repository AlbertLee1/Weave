package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
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

// oidcLogoutHarness stands up an OIDC back-channel logout handler wired
// against a real oidctest.Server so the verifier runs actual JWT signature
// + iss / aud / exp validation — the checks that live upstream of the
// handler's claim-shape enforcement. Returns helpers for signing arbitrary
// claims and for asserting session + refresh state after a request.
type oidcLogoutHarness struct {
	handler      *OIDCBackChannelLogoutHandler
	signClaims   func(map[string]interface{}) string
	users        *fakeUserRepo
	sessions     *MemorySessionStore
	refreshStore *MemoryRefreshStore
	issuer       string
	clientID     string
}

func newOIDCLogoutHarness(t *testing.T) *oidcLogoutHarness {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	testSrv := &oidctest.Server{
		PublicKeys: []oidctest.PublicKey{{
			PublicKey: priv.Public(),
			KeyID:     "test-key",
			Algorithm: oidc.RS256,
		}},
	}
	httpSrv := httptest.NewServer(testSrv)
	t.Cleanup(httpSrv.Close)
	testSrv.SetIssuer(httpSrv.URL)

	ctx := context.Background()
	provider, err := oidc.NewProvider(ctx, httpSrv.URL)
	if err != nil {
		t.Fatalf("oidc.NewProvider: %v", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: "test-client"})

	users := newFakeUserRepo()
	sessions := NewMemorySessionStore()
	refreshStore := NewMemoryRefreshStore()
	rs := NewRefreshService(refreshStore, RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})

	h := NewOIDCBackChannelLogoutHandler(OIDCBackChannelLogoutDeps{
		Verifier:       verifier,
		ClientID:       "test-client",
		Users:          users,
		SessionStore:   sessions,
		RefreshService: rs,
	})

	sign := func(claims map[string]interface{}) string {
		if _, ok := claims["iss"]; !ok {
			claims["iss"] = httpSrv.URL
		}
		if _, ok := claims["aud"]; !ok {
			claims["aud"] = "test-client"
		}
		if _, ok := claims["iat"]; !ok {
			claims["iat"] = time.Now().Unix()
		}
		if _, ok := claims["exp"]; !ok {
			claims["exp"] = time.Now().Add(5 * time.Minute).Unix()
		}
		raw, err := json.Marshal(claims)
		if err != nil {
			t.Fatal(err)
		}
		return oidctest.SignIDToken(priv, "test-key", oidc.RS256, string(raw))
	}

	return &oidcLogoutHarness{
		handler:      h,
		signClaims:   sign,
		users:        users,
		sessions:     sessions,
		refreshStore: refreshStore,
		issuer:       httpSrv.URL,
		clientID:     "test-client",
	}
}

// goodLogoutClaims is a valid OIDC back-channel logout token body.
func goodLogoutClaims() map[string]interface{} {
	return map[string]interface{}{
		"sub":   "alice@example.com",
		"email": "alice@example.com",
		"jti":   "logout-jti-1",
		"events": map[string]interface{}{
			BackChannelLogoutEventKey: map[string]interface{}{},
		},
	}
}

// seedActiveSession creates a user + one refresh token + one session row
// so the handler has state to tear down. Returns the resulting userID.
func seedActiveSession(t *testing.T, h *oidcLogoutHarness, email string) string {
	t.Helper()
	user := &UserRecord{ID: "user:" + email, Email: email, Name: "Alice"}
	if err := h.users.CreateUser(context.Background(), user); err != nil {
		t.Fatal(err)
	}
	refresh := &RefreshTokenRecord{
		ID:        "rt-" + email,
		UserID:    user.ID,
		TokenHash: "hash-" + email,
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := h.refreshStore.Create(context.Background(), refresh); err != nil {
		t.Fatal(err)
	}
	sess := &SessionRecord{
		ID:             "sess-" + email,
		UserID:         user.ID,
		RefreshTokenID: refresh.ID,
	}
	if err := h.sessions.Create(context.Background(), sess); err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func postLogoutToken(t *testing.T, h *OIDCBackChannelLogoutHandler, token string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{}
	form.Set("logout_token", token)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/oidc/back-channel-logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestOIDCBackChannelLogout_HappyPath_ClearsSessionAndRefresh(t *testing.T) {
	h := newOIDCLogoutHarness(t)
	userID := seedActiveSession(t, h, "alice@example.com")

	rec := postLogoutToken(t, h.handler, h.signClaims(goodLogoutClaims()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control=%q, want no-store", rec.Header().Get("Cache-Control"))
	}

	sessions, err := h.sessions.ListByUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("sessions not cleared after logout: %d remaining", len(sessions))
	}

	rec2, err := h.refreshStore.GetByHash(context.Background(), "hash-alice@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if !rec2.IsRevoked() {
		t.Fatal("refresh token not revoked")
	}
	if rec2.RevocationReason != "oidc_back_channel_logout" {
		t.Fatalf("revocation reason=%q, want oidc_back_channel_logout", rec2.RevocationReason)
	}
}

func TestOIDCBackChannelLogout_RejectsNonPOST(t *testing.T) {
	h := newOIDCLogoutHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/back-channel-logout", nil)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("GET should not return 200")
	}
}

func TestOIDCBackChannelLogout_RejectsMissingToken(t *testing.T) {
	h := newOIDCLogoutHarness(t)
	form := url.Values{}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/oidc/back-channel-logout", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCLogoutMissingToken") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOIDCBackChannelLogout_RejectsInvalidSignature(t *testing.T) {
	h := newOIDCLogoutHarness(t)
	// Sign with a throwaway key — the harness's Verifier is bound to a
	// different JWKS, so signature verification fails.
	otherPriv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	claims := goodLogoutClaims()
	claims["iss"] = h.issuer
	claims["aud"] = h.clientID
	claims["iat"] = time.Now().Unix()
	raw, _ := json.Marshal(claims)
	token := oidctest.SignIDToken(otherPriv, "other-key", oidc.RS256, string(raw))

	rec := postLogoutToken(t, h.handler, token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCLogoutTokenInvalid") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOIDCBackChannelLogout_RejectsNonceClaim(t *testing.T) {
	h := newOIDCLogoutHarness(t)
	claims := goodLogoutClaims()
	claims["nonce"] = "should-not-be-here"

	rec := postLogoutToken(t, h.handler, h.signClaims(claims))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCLogoutClaimsInvalid") {
		t.Fatalf("body=%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "nonce") {
		t.Fatalf("reason should mention nonce: %s", rec.Body.String())
	}
}

func TestOIDCBackChannelLogout_RejectsMissingEventsClaim(t *testing.T) {
	h := newOIDCLogoutHarness(t)
	claims := goodLogoutClaims()
	delete(claims, "events")

	rec := postLogoutToken(t, h.handler, h.signClaims(claims))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "OIDCLogoutClaimsInvalid") {
		t.Fatalf("body=%s", rec.Body.String())
	}
}

func TestOIDCBackChannelLogout_RejectsWrongEventsClaim(t *testing.T) {
	h := newOIDCLogoutHarness(t)
	claims := goodLogoutClaims()
	claims["events"] = map[string]interface{}{
		"http://schemas.openid.net/event/some-other-event": map[string]interface{}{},
	}

	rec := postLogoutToken(t, h.handler, h.signClaims(claims))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
}

func TestOIDCBackChannelLogout_RejectsMissingSubAndSid(t *testing.T) {
	h := newOIDCLogoutHarness(t)
	claims := goodLogoutClaims()
	delete(claims, "sub")

	rec := postLogoutToken(t, h.handler, h.signClaims(claims))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401. body=%s", rec.Code, rec.Body.String())
	}
}

func TestOIDCBackChannelLogout_UnknownUser_Returns200Idempotently(t *testing.T) {
	// No user seeded. The spec expects 200 regardless so the IdP can't
	// probe for subject existence.
	h := newOIDCLogoutHarness(t)
	claims := goodLogoutClaims()
	claims["sub"] = "stranger@example.com"
	claims["email"] = "stranger@example.com"

	rec := postLogoutToken(t, h.handler, h.signClaims(claims))
	if rec.Code != http.StatusOK {
		t.Fatalf("unknown user should return 200, got %d", rec.Code)
	}
}

func TestOIDCBackChannelLogout_SidOnlyToken_ValidatesShape(t *testing.T) {
	// sid-only tokens are valid per spec (sub OR sid). We don't currently
	// map sid → session row (bulk-by-user is the fallback), so a sid-only
	// token still returns 200 but has nothing to revoke — the shape check
	// must not reject it.
	h := newOIDCLogoutHarness(t)
	claims := map[string]interface{}{
		"sid": "upstream-session-id",
		"jti": "logout-sid-1",
		"events": map[string]interface{}{
			BackChannelLogoutEventKey: map[string]interface{}{},
		},
	}

	rec := postLogoutToken(t, h.handler, h.signClaims(claims))
	if rec.Code != http.StatusOK {
		t.Fatalf("sid-only token should validate, got %d. body=%s", rec.Code, rec.Body.String())
	}
}

func TestOIDCBackChannelLogout_OnlySubClaim_ResolvesViaUserID(t *testing.T) {
	// Verify the sub → user:<email> fallback when the email claim is
	// omitted but sub is email-shaped.
	h := newOIDCLogoutHarness(t)
	userID := seedActiveSession(t, h, "bob@example.com")

	claims := map[string]interface{}{
		"sub": "bob@example.com",
		"jti": "logout-sub-1",
		"events": map[string]interface{}{
			BackChannelLogoutEventKey: map[string]interface{}{},
		},
	}

	rec := postLogoutToken(t, h.handler, h.signClaims(claims))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200. body=%s", rec.Code, rec.Body.String())
	}
	sessions, _ := h.sessions.ListByUser(context.Background(), userID)
	if len(sessions) != 0 {
		t.Fatalf("sessions not cleared: %d remaining", len(sessions))
	}
}

func TestOIDCBackChannelLogout_OnlyAffectsNamedUser(t *testing.T) {
	// Ensure bulk revocation is user-scoped, not global. Two users, two
	// sessions; a logout token for one must leave the other untouched.
	h := newOIDCLogoutHarness(t)
	aliceID := seedActiveSession(t, h, "alice@example.com")
	bobID := seedActiveSession(t, h, "bob@example.com")

	rec := postLogoutToken(t, h.handler, h.signClaims(goodLogoutClaims()))
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rec.Code)
	}

	aliceSessions, _ := h.sessions.ListByUser(context.Background(), aliceID)
	if len(aliceSessions) != 0 {
		t.Fatalf("alice session not cleared: %d", len(aliceSessions))
	}
	bobSessions, _ := h.sessions.ListByUser(context.Background(), bobID)
	if len(bobSessions) != 1 {
		t.Fatalf("bob session wrongly cleared: %d, want 1", len(bobSessions))
	}
	bobRefresh, _ := h.refreshStore.GetByHash(context.Background(), "hash-bob@example.com")
	if bobRefresh.IsRevoked() {
		t.Fatal("bob refresh token wrongly revoked")
	}
}

// helpers

func findOIDCLogoutRoute(t *testing.T, h *OIDCBackChannelLogoutHandler) string {
	t.Helper()
	got := ""
	mux := &routeCollector{}
	mux.collect = func(method, pattern string, _ http.Handler) {
		if method == http.MethodPost {
			got = pattern
		}
	}
	h.RegisterRoutes(mux)
	return got
}

type routeCollector struct {
	collect func(method, pattern string, handler http.Handler)
}

func (r *routeCollector) Method(method, pattern string, handler http.Handler) {
	r.collect(method, pattern, handler)
}

func TestOIDCBackChannelLogout_RegisterRoutes_MountsPath(t *testing.T) {
	h := newOIDCLogoutHarness(t)
	got := findOIDCLogoutRoute(t, h.handler)
	if got != "/api/auth/oidc/back-channel-logout" {
		t.Fatalf("route=%q, want /api/auth/oidc/back-channel-logout", got)
	}
}
