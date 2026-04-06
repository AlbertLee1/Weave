package main

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

	"github.com/liyang/weave/pkg/auth"
)

// fakeUserRepo is a tiny in-memory UserRepository sufficient for wiring up
// the full router in cmd/server tests.
type fakeUserRepo struct {
	users map[string]*auth.UserRecord
	roles map[string][]string
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{
		users: map[string]*auth.UserRecord{},
		roles: map[string][]string{},
	}
}

func (f *fakeUserRepo) CreateUser(_ context.Context, u *auth.UserRecord) error {
	f.users[u.ID] = u
	return nil
}
func (f *fakeUserRepo) GetUserByID(_ context.Context, id string) (*auth.UserRecord, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, auth.ErrUserNotFound
	}
	return u, nil
}
func (f *fakeUserRepo) GetUserByEmail(_ context.Context, email string) (*auth.UserRecord, error) {
	for _, u := range f.users {
		if u.Email == email {
			return u, nil
		}
	}
	return nil, auth.ErrUserNotFound
}
func (f *fakeUserRepo) ListUserRoles(_ context.Context, id string) ([]string, error) {
	return append([]string(nil), f.roles[id]...), nil
}
func (f *fakeUserRepo) ListUserOntologyRoles(_ context.Context, _ string) (map[string]string, error) {
	return map[string]string{}, nil
}
func (f *fakeUserRepo) UpsertUserRole(_ context.Context, id, role string) error {
	f.roles[id] = append(f.roles[id], role)
	return nil
}
func (f *fakeUserRepo) SetPassword(_ context.Context, id, hash string) error {
	u, ok := f.users[id]
	if !ok {
		return auth.ErrUserNotFound
	}
	u.PasswordHash = hash
	return nil
}

func setupAuthRoutesHarness(t *testing.T) (*ServerDeps, *fakeUserRepo) {
	t.Helper()
	repo := newFakeUserRepo()
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer, _ := auth.NewJWTSigner(priv, &priv.PublicKey, auth.JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	rs := auth.NewRefreshService(auth.NewMemoryRefreshStore(),
		auth.RefreshServiceOptions{AbsoluteTTL: 7 * 24 * time.Hour})
	deps := &ServerDeps{
		UserRepo:       repo,
		RoleResolver:   auth.NewRoleResolver(repo, time.Minute),
		JWTSigner:      signer,
		RefreshService: rs,
	}
	return deps, repo
}

func TestAuthRoutes_LoginRefreshLogout(t *testing.T) {
	deps, repo := setupAuthRoutesHarness(t)

	hash, _ := auth.HashPassword("letmein123!")
	repo.users["user:alice@example.com"] = &auth.UserRecord{
		ID:           "user:alice@example.com",
		Email:        "alice@example.com",
		Name:         "Alice",
		PasswordHash: hash,
	}
	repo.roles["user:alice@example.com"] = []string{"editor"}

	router := NewFullRouter(deps)

	// 1. Login
	body, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "letmein123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var loginResp auth.LoginResponse
	if err := json.NewDecoder(rec.Body).Decode(&loginResp); err != nil {
		t.Fatal(err)
	}
	if loginResp.AccessToken == "" || loginResp.RefreshToken == "" {
		t.Fatal("expected access and refresh tokens")
	}

	// 2. Refresh
	body2, _ := json.Marshal(map[string]string{"refresh_token": loginResp.RefreshToken})
	req2 := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("refresh: status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var refreshResp auth.LoginResponse
	json.NewDecoder(rec2.Body).Decode(&refreshResp)
	if refreshResp.RefreshToken == "" || refreshResp.RefreshToken == loginResp.RefreshToken {
		t.Errorf("expected new rotated refresh token")
	}

	// 3. Logout the new refresh
	body3, _ := json.Marshal(map[string]string{"refresh_token": refreshResp.RefreshToken})
	req3 := httptest.NewRequest(http.MethodPost, "/api/auth/logout", bytes.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNoContent {
		t.Errorf("logout: expected 204, got %d", rec3.Code)
	}

	// 4. After logout, refresh should fail.
	body4, _ := json.Marshal(map[string]string{"refresh_token": refreshResp.RefreshToken})
	req4 := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewReader(body4))
	req4.Header.Set("Content-Type", "application/json")
	rec4 := httptest.NewRecorder()
	router.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusUnauthorized {
		t.Errorf("post-logout refresh: expected 401, got %d", rec4.Code)
	}
}

func TestAuthRoutes_LoginRoutesAreUnauthenticated(t *testing.T) {
	// AUTH_MODE=jwt with a signer; login route MUST NOT require a Bearer token.
	t.Setenv("AUTH_MODE", "jwt")
	deps, repo := setupAuthRoutesHarness(t)
	hash, _ := auth.HashPassword("letmein123!")
	repo.users["user:alice@example.com"] = &auth.UserRecord{
		ID: "user:alice@example.com", Email: "alice@example.com", PasswordHash: hash,
	}
	router := NewFullRouter(deps)

	body, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "letmein123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 on unauth login under jwt mode, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAuthRoutes_JWTMeRoundTrip(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	deps, repo := setupAuthRoutesHarness(t)

	hash, _ := auth.HashPassword("letmein123!")
	repo.users["user:alice@example.com"] = &auth.UserRecord{
		ID:           "user:alice@example.com",
		Email:        "alice@example.com",
		Name:         "Alice",
		PasswordHash: hash,
	}
	repo.roles["user:alice@example.com"] = []string{"editor"}

	router := NewFullRouter(deps)

	// Login.
	body, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "letmein123!",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var loginResp auth.LoginResponse
	json.NewDecoder(rec.Body).Decode(&loginResp)

	// Use the access token to call /api/v2/me.
	meReq := httptest.NewRequest(http.MethodGet, "/api/v2/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	meRec := httptest.NewRecorder()
	router.ServeHTTP(meRec, meReq)
	if meRec.Code != http.StatusOK {
		t.Fatalf("me: status=%d body=%s", meRec.Code, meRec.Body.String())
	}
	var me map[string]any
	json.NewDecoder(meRec.Body).Decode(&me)
	if me["id"] != "user:alice@example.com" {
		t.Errorf("me.id: got %v", me["id"])
	}
	roles, _ := me["roles"].([]any)
	if len(roles) != 1 || roles[0] != "editor" {
		t.Errorf("me.roles: got %v", me["roles"])
	}
}
