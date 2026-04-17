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

// TestAuthorizationCodeFlow_FullRoundTrip exercises the full US-142
// acceptance criterion: authorize → redirect-with-code → token exchange →
// valid access_token → scoped protected-API access. Uses a real chi router
// with auth.MiddlewareFull so the "auth middleware checks scope
// intersection" behaviour is exercised end-to-end, not just in unit tests.
func TestAuthorizationCodeFlow_FullRoundTrip(t *testing.T) {
	h := newOAuthHarness(t)
	authenticator := NewOAuthAuthenticator(h.tokens).WithClock(func() time.Time { return h.now })

	// Build a router that exposes the OAuth endpoints (unauthed) plus a
	// single protected /api/objects probe behind MiddlewareFull +
	// RequireOAuthScope("read:objects").
	router := chi.NewRouter()
	h.h.RegisterRoutes(router)
	router.Group(func(r chi.Router) {
		r.Use(auth.MiddlewareFull(nil, nil, nil, nil, authenticator))
		r.With(auth.RequireOAuthScope("read:objects")).
			Get("/api/objects", func(w http.ResponseWriter, r *http.Request) {
				u := auth.UserFromContext(r.Context())
				scopes := auth.OAuthScopes(r.Context())
				_ = json.NewEncoder(w).Encode(map[string]any{
					"userId": u.ID,
					"scopes": scopes,
				})
			})
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	// Step 1: approve consent, capture the redirect code.
	verifier := strings.Repeat("a", 43)
	code := issueCodeFor(t, h, "read:objects", verifier)

	// Step 2: exchange the code for an access_token.
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("client_id", h.app.ClientID)
	form.Set("code_verifier", verifier)
	exchangeResp, err := http.Post(srv.URL+"/oauth/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("token exchange: %v", err)
	}
	defer exchangeResp.Body.Close()
	if exchangeResp.StatusCode != http.StatusOK {
		t.Fatalf("token exchange status: %d", exchangeResp.StatusCode)
	}
	var tokResp TokenResponse
	if err := json.NewDecoder(exchangeResp.Body).Decode(&tokResp); err != nil {
		t.Fatalf("decode token: %v", err)
	}

	// Step 3: call the protected API with the access_token — should pass
	// the scope check and echo the scopes back.
	apiReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/objects", nil)
	apiReq.Header.Set("Authorization", "Bearer "+tokResp.AccessToken)
	apiResp, err := http.DefaultClient.Do(apiReq)
	if err != nil {
		t.Fatalf("api call: %v", err)
	}
	defer apiResp.Body.Close()
	if apiResp.StatusCode != http.StatusOK {
		t.Fatalf("protected api status: %d", apiResp.StatusCode)
	}
	var payload struct {
		UserID string   `json:"userId"`
		Scopes []string `json:"scopes"`
	}
	if err := json.NewDecoder(apiResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode api: %v", err)
	}
	if payload.UserID != "user:alice@example.com" {
		t.Errorf("user id: got %q", payload.UserID)
	}
	if len(payload.Scopes) != 1 || payload.Scopes[0] != "read:objects" {
		t.Errorf("scopes: got %v", payload.Scopes)
	}
}

// TestAuthorizationCodeFlow_InsufficientScopeRejected verifies the second
// half of the acceptance criterion: a token that does NOT carry the
// required scope is rejected by RequireOAuthScope.
func TestAuthorizationCodeFlow_InsufficientScopeRejected(t *testing.T) {
	h := newOAuthHarness(t)
	authenticator := NewOAuthAuthenticator(h.tokens).WithClock(func() time.Time { return h.now })

	router := chi.NewRouter()
	h.h.RegisterRoutes(router)
	router.Group(func(r chi.Router) {
		r.Use(auth.MiddlewareFull(nil, nil, nil, nil, authenticator))
		r.With(auth.RequireOAuthScope("admin:ontology")).
			Get("/api/admin", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	verifier := strings.Repeat("a", 43)
	code := issueCodeFor(t, h, "read:objects", verifier) // NOT admin:ontology

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", "https://client.example.com/cb")
	form.Set("client_id", h.app.ClientID)
	form.Set("code_verifier", verifier)
	exResp, err := http.Post(srv.URL+"/oauth/token", "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("token exchange: %v", err)
	}
	defer exResp.Body.Close()
	var tok TokenResponse
	_ = json.NewDecoder(exResp.Body).Decode(&tok)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/admin", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("api call: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 InsufficientScope, got %d", resp.StatusCode)
	}
}

// TestOAuthAuthenticator_RejectsRevokedToken drives the bottom half of the
// middleware: even a well-formed bearer is rejected if the row has been
// revoked.
func TestOAuthAuthenticator_RejectsRevokedToken(t *testing.T) {
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
	var resp TokenResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	// Revoke every access token we just issued.
	for _, rows := range h.tokens.byPrefix {
		for _, row := range rows {
			if row.TokenType == TokenTypeAccess {
				_ = h.tokens.Revoke(nil, row.ID, h.now)
			}
		}
	}

	authenticator := NewOAuthAuthenticator(h.tokens).WithClock(func() time.Time { return h.now })
	if _, err := authenticator.ValidateOAuthAccessToken(nil, resp.AccessToken); err == nil {
		t.Errorf("expected revoked token to fail validation")
	}
}
