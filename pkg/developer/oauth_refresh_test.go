package developer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// exchangeCodeForTokens drives a full authorize-then-token round-trip and
// returns the resulting TokenResponse. Helper for the refresh tests so each
// case starts with a freshly minted (access, refresh) pair.
func exchangeCodeForTokens(t *testing.T, h *oauthHarness, scopes string) TokenResponse {
	t.Helper()
	verifier := strings.Repeat("a", 43)
	code := issueCodeFor(t, h, scopes, verifier)
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
		t.Fatalf("authcode exchange: %d %s", rec.Code, rec.Body.String())
	}
	var resp TokenResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode tokens: %v", err)
	}
	return resp
}

// postRefresh issues a refresh-token grant via the harness's Token handler,
// authenticating the (confidential) client with HTTP Basic auth.
func postRefresh(t *testing.T, h *oauthHarness, form url.Values) (*httptest.ResponseRecorder, TokenResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(h.app.ClientID, h.secret)
	rec := httptest.NewRecorder()
	h.h.Token(rec, req)
	var resp TokenResponse
	if rec.Code == http.StatusOK {
		_ = json.NewDecoder(rec.Body).Decode(&resp)
	}
	return rec, resp
}

func TestToken_RefreshTokenFlow_RotatesAndReturnsNewPair(t *testing.T) {
	h := newOAuthHarness(t)
	original := exchangeCodeForTokens(t, h, "read:objects")

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", original.RefreshToken)
	form.Set("client_id", h.app.ClientID)

	rec, resp := postRefresh(t, h, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(resp.AccessToken, AccessTokenMarker) {
		t.Errorf("new access_token shape: %q", resp.AccessToken)
	}
	if !strings.HasPrefix(resp.RefreshToken, RefreshTokenMarker) {
		t.Errorf("new refresh_token shape: %q", resp.RefreshToken)
	}
	if resp.AccessToken == original.AccessToken {
		t.Errorf("access_token must be freshly minted on refresh")
	}
	if resp.RefreshToken == original.RefreshToken {
		t.Errorf("refresh_token must rotate (security best practice RFC 9700)")
	}
	if resp.Scope != "read:objects" {
		t.Errorf("scope round-trip: got %q", resp.Scope)
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type: %q", resp.TokenType)
	}
}

func TestToken_RefreshTokenFlow_OldRefreshTokenIsRevoked(t *testing.T) {
	h := newOAuthHarness(t)
	original := exchangeCodeForTokens(t, h, "read:objects")

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", original.RefreshToken)
	form.Set("client_id", h.app.ClientID)

	if rec, _ := postRefresh(t, h, form); rec.Code != http.StatusOK {
		t.Fatalf("first refresh: %d %s", rec.Code, rec.Body.String())
	}

	// Replay the SAME refresh token — must fail (rotation single-use).
	rec2, _ := postRefresh(t, h, form)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("replay refresh: expected 400, got %d", rec2.Code)
	}
	var errResp oauthTokenError
	_ = json.NewDecoder(rec2.Body).Decode(&errResp)
	if errResp.Error != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %q", errResp.Error)
	}
}

func TestToken_RefreshTokenFlow_NarrowsScopeWhenRequested(t *testing.T) {
	h := newOAuthHarness(t)
	original := exchangeCodeForTokens(t, h, "read:objects write:objects")

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", original.RefreshToken)
	form.Set("client_id", h.app.ClientID)
	form.Set("scope", "read:objects") // narrow

	rec, resp := postRefresh(t, h, form)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh narrowing: %d %s", rec.Code, rec.Body.String())
	}
	if resp.Scope != "read:objects" {
		t.Errorf("expected narrowed scope, got %q", resp.Scope)
	}
}

func TestToken_RefreshTokenFlow_RejectsScopeExpansion(t *testing.T) {
	h := newOAuthHarness(t)
	original := exchangeCodeForTokens(t, h, "read:objects") // only read

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", original.RefreshToken)
	form.Set("client_id", h.app.ClientID)
	form.Set("scope", "read:objects write:objects") // attempts to widen

	rec, _ := postRefresh(t, h, form)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("scope expansion: expected 400, got %d", rec.Code)
	}
	var errResp oauthTokenError
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp.Error != "invalid_scope" {
		t.Errorf("expected invalid_scope, got %q", errResp.Error)
	}
}

func TestToken_RefreshTokenFlow_RejectsClientMismatch(t *testing.T) {
	h := newOAuthHarness(t)
	original := exchangeCodeForTokens(t, h, "read:objects")

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", original.RefreshToken)
	form.Set("client_id", "wapp_OTHER0000000000000000000")

	rec, _ := postRefresh(t, h, form)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("client mismatch: expected 400, got %d", rec.Code)
	}
	var errResp oauthTokenError
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp.Error != "invalid_grant" {
		t.Errorf("expected invalid_grant, got %q", errResp.Error)
	}
}

func TestToken_RefreshTokenFlow_RejectsRevokedRefresh(t *testing.T) {
	h := newOAuthHarness(t)
	original := exchangeCodeForTokens(t, h, "read:objects")

	// Pre-revoke the refresh token row.
	for _, rows := range h.tokens.byPrefix {
		for _, row := range rows {
			if row.TokenType == TokenTypeRefresh {
				_ = h.tokens.Revoke(context.Background(), row.ID, h.now)
			}
		}
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", original.RefreshToken)
	form.Set("client_id", h.app.ClientID)

	rec, _ := postRefresh(t, h, form)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("revoked refresh: expected 400, got %d", rec.Code)
	}
}

func TestToken_RefreshTokenFlow_RejectsAccessTokenAsRefresh(t *testing.T) {
	h := newOAuthHarness(t)
	original := exchangeCodeForTokens(t, h, "read:objects")

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", original.AccessToken) // wrong type — wvoa_ not wvor_
	form.Set("client_id", h.app.ClientID)

	rec, _ := postRefresh(t, h, form)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("access-as-refresh: expected 400, got %d", rec.Code)
	}
}

func TestToken_RefreshTokenFlow_PublicClientNoSecret(t *testing.T) {
	// Public clients (SPAs / mobile) register without a client_secret. The
	// refresh endpoint must accept refresh from such a client identified by
	// client_id alone — matching the PKCE / SPA contract.
	h := newOAuthHarness(t)
	// Drop the secret so the test app is treated as public.
	h.app.ClientSecretHash = nil
	h.apps.byClientID[h.app.ClientID] = h.app
	h.apps.byID[h.app.ID] = h.app

	original := exchangeCodeForTokens(t, h, "read:objects")

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", original.RefreshToken)
	form.Set("client_id", h.app.ClientID)
	// NOTE: no Basic auth, no client_secret.

	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.h.Token(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("public-client refresh: %d %s", rec.Code, rec.Body.String())
	}
	var resp TokenResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.AccessToken == original.AccessToken {
		t.Errorf("public-client refresh did not rotate access_token")
	}
	if resp.RefreshToken == original.RefreshToken {
		t.Errorf("public-client refresh did not rotate refresh_token")
	}
}

func TestToken_RefreshTokenFlow_RejectsMissingRefreshToken(t *testing.T) {
	h := newOAuthHarness(t)
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", h.app.ClientID)
	// no refresh_token

	rec, _ := postRefresh(t, h, form)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing refresh_token: expected 400, got %d", rec.Code)
	}
	var errResp oauthTokenError
	_ = json.NewDecoder(rec.Body).Decode(&errResp)
	if errResp.Error != "invalid_request" {
		t.Errorf("expected invalid_request, got %q", errResp.Error)
	}
}
