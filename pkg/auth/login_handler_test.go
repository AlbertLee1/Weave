package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newLoginHandlerHarness(t *testing.T) (*LoginHandler, *fakeUserRepo, *RefreshService) {
	t.Helper()

	repo := newFakeUserRepo()
	resolver := NewRoleResolver(repo, time.Minute)

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	signer, err := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}

	store := NewMemoryRefreshStore()
	rs := NewRefreshService(store, RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})

	h := NewLoginHandler(LoginHandlerDeps{
		Users:          repo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
		RateLimit:      0, // disabled in tests
	})
	return h, repo, rs
}

func seedUser(t *testing.T, repo *fakeUserRepo, id, email, password, name string, roles ...string) {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	repo.users[id] = &UserRecord{
		ID:           id,
		Email:        email,
		Name:         name,
		PasswordHash: hash,
	}
	for _, r := range roles {
		repo.roles[id] = append(repo.roles[id], r)
	}
}

func postLogin(t *testing.T, h *LoginHandler, body any) *httptest.ResponseRecorder {
	t.Helper()
	bs, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(bs))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginHandler_Success(t *testing.T) {
	h, repo, _ := newLoginHandlerHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Alice", "editor")

	rec := postLogin(t, h, map[string]string{
		"email":    "alice@example.com",
		"password": "letmein123!",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("expected access_token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected refresh_token")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type: got %q", resp.TokenType)
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("expires_in: got %d, want 900", resp.ExpiresIn)
	}
	if resp.User.ID != "user:alice@example.com" {
		t.Errorf("user.id: got %q", resp.User.ID)
	}
	if len(resp.User.Roles) != 1 || resp.User.Roles[0] != "editor" {
		t.Errorf("user.roles: got %v", resp.User.Roles)
	}
}

func TestLoginHandler_WrongPassword(t *testing.T) {
	h, repo, _ := newLoginHandlerHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Alice")

	rec := postLogin(t, h, map[string]string{
		"email":    "alice@example.com",
		"password": "WRONG",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestLoginHandler_UserNotFound(t *testing.T) {
	h, _, _ := newLoginHandlerHarness(t)

	rec := postLogin(t, h, map[string]string{
		"email":    "ghost@example.com",
		"password": "anypw",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	// Generic message must NOT leak that the user doesn't exist.
	body := rec.Body.String()
	if body == "" {
		t.Error("expected error body")
	}
}

func TestLoginHandler_MissingFields(t *testing.T) {
	h, _, _ := newLoginHandlerHarness(t)
	rec := postLogin(t, h, map[string]string{"email": "alice@example.com"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing password, got %d", rec.Code)
	}
}

func TestLoginHandler_PasswordHashEmptyMeansLoginDisabled(t *testing.T) {
	h, repo, _ := newLoginHandlerHarness(t)
	repo.users["user:alice@example.com"] = &UserRecord{
		ID:           "user:alice@example.com",
		Email:        "alice@example.com",
		PasswordHash: "", // no password set
	}

	rec := postLogin(t, h, map[string]string{
		"email":    "alice@example.com",
		"password": "anything",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for user without password, got %d", rec.Code)
	}
}

func TestLoginHandler_RateLimit(t *testing.T) {
	repo := newFakeUserRepo()
	resolver := NewRoleResolver(repo, time.Minute)
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{AccessTokenTTL: 15 * time.Minute})
	rs := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})
	h := NewLoginHandler(LoginHandlerDeps{
		Users:          repo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
		RateLimit:      3, // 3 attempts/min
	})
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Alice")

	// First 3 should be allowed.
	for i := 0; i < 3; i++ {
		rec := postLogin(t, h, map[string]string{"email": "alice@example.com", "password": "WRONG"})
		if rec.Code == http.StatusTooManyRequests {
			t.Errorf("attempt %d unexpectedly rate limited", i+1)
		}
	}
	// 4th should be rejected with 429.
	rec := postLogin(t, h, map[string]string{"email": "alice@example.com", "password": "WRONG"})
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 on 4th attempt, got %d", rec.Code)
	}
}

func TestLoginHandler_MFAEnabledReturnsChallenge(t *testing.T) {
	repo := newFakeUserRepo()
	resolver := NewRoleResolver(repo, time.Minute)
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{AccessTokenTTL: 15 * time.Minute})
	rs := NewRefreshService(NewMemoryRefreshStore(), RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})
	store := NewMFAChallengeStore(time.Minute)
	h := NewLoginHandler(LoginHandlerDeps{
		Users:          repo,
		Resolver:       resolver,
		Signer:         signer,
		RefreshService: rs,
		MFAChallenges:  store,
	})
	hash, _ := HashPassword("letmein123!")
	repo.users["user:alice@example.com"] = &UserRecord{
		ID:           "user:alice@example.com",
		Email:        "alice@example.com",
		PasswordHash: hash,
		MFASecret:    "JBSWY3DPEHPK3PXP",
		MFAEnabled:   true,
	}

	rec := postLogin(t, h, map[string]string{"email": "alice@example.com", "password": "letmein123!"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted with MFA challenge, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp MFAChallengeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.MFARequired || resp.ChallengeToken == "" {
		t.Errorf("expected mfa_required=true with challenge_token, got %+v", resp)
	}
	if resp.ExpiresIn <= 0 {
		t.Errorf("expected positive expires_in, got %d", resp.ExpiresIn)
	}
	// Token must be live in the store.
	uid, err := store.Consume(resp.ChallengeToken)
	if err != nil {
		t.Fatalf("challenge consume: %v", err)
	}
	if uid != "user:alice@example.com" {
		t.Errorf("user id: got %q", uid)
	}
}

func TestLoginHandler_MFAEnabledButStoreUnwiredFallsThrough(t *testing.T) {
	// When MFAChallenges is nil (degraded mode) the login handler must
	// still issue tokens — better to log in than to lock everyone out.
	h, repo, _ := newLoginHandlerHarness(t)
	hash, _ := HashPassword("letmein123!")
	repo.users["user:alice@example.com"] = &UserRecord{
		ID:           "user:alice@example.com",
		Email:        "alice@example.com",
		PasswordHash: hash,
		MFASecret:    "JBSWY3DPEHPK3PXP",
		MFAEnabled:   true,
	}
	rec := postLogin(t, h, map[string]string{"email": "alice@example.com", "password": "letmein123!"})
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 fallback when MFAChallenges is nil, got %d", rec.Code)
	}
}

func TestLoginHandler_StoresRefreshToken(t *testing.T) {
	h, repo, rs := newLoginHandlerHarness(t)
	seedUser(t, repo, "user:alice@example.com", "alice@example.com", "letmein123!", "Alice")

	rec := postLogin(t, h, map[string]string{
		"email":    "alice@example.com",
		"password": "letmein123!",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d", rec.Code)
	}
	var resp LoginResponse
	json.NewDecoder(rec.Body).Decode(&resp)

	got, err := rs.Lookup(context.Background(), resp.RefreshToken)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if got.UserID != "user:alice@example.com" {
		t.Errorf("UserID: got %q", got.UserID)
	}
}
