//go:build integration

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

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
)

// US-491 — JWT token 撤销 + 黑名单 (BDD).
//
// PRD 验收：
//   - 表 auth_revoked_tokens(jti, expires_at)；middleware 查 + cache
//   - POST /api/auth/tokens/{jti}/revoke
//   - 测试：撤销后再请求 401
//
// 走完整 wire surface:
//   - 真 testcontainers PG (auth_revoked_tokens 表来自 migration 000211)
//   - 真 chi router + auth.MiddlewareFullWithRevocation
//   - 真 RSA 签名 / 真 JWT verify, JWT mode
//   - PG-backed RevocationStore + CachedRevocationChecker (30s TTL，但 admin 路径 Invalidate)
//
// Scenario A (PRD literal — 撤销后再请求 401):
//   Given a server with a fresh PG-backed RevocationStore and a JWT minted for admin
//   When admin POSTs /api/auth/tokens/{jti}/revoke for the token's own jti
//   Then the response is 200 carrying {jti, revokedAt}
//    And the auth_revoked_tokens table has a row for that jti (raw SQL read)
//    And the SAME token replayed at /protected now returns 401 TokenRevoked
//
// Scenario B (negative control — non-revoked token still passes):
//   Given an unrelated JWT minted under the same signer
//   When it is replayed at /protected
//   Then the request is admitted with 200 (proves the middleware did not become
//      a deny-everything regression)
//
// Scenario C (negative control — admin gate ON the revoke endpoint):
//   Given a JWT minted for a non-admin user
//   When the user POSTs /api/auth/tokens/{jti}/revoke
//   Then the request is rejected 403 AND no row is inserted (raw SQL count = 0).

func setupUS491BDDFixture(t *testing.T) (
	router *chi.Mux,
	pg *testutil.PGContainer,
	signer *auth.JWTSigner,
	mintAdminToken func() (rawToken string, jti string, exp time.Time),
	mintUserToken func() (rawToken string, jti string, exp time.Time),
) {
	t.Helper()
	t.Setenv("AUTH_MODE", "jwt")

	pg = testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("RunMigrationsUp: %v", err)
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa gen: %v", err)
	}
	signer, err = auth.NewJWTSigner(priv, &priv.PublicKey, auth.JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 30 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}

	mintFn := func(userID string, roles []string) (string, string, time.Time) {
		raw, err := signer.Sign(auth.SignInput{
			UserID: userID,
			Roles:  roles,
		})
		if err != nil {
			t.Fatalf("Sign(%s): %v", userID, err)
		}
		claims, err := signer.Verify(raw)
		if err != nil {
			t.Fatalf("Verify(%s): %v", userID, err)
		}
		return raw, claims.ID, claims.ExpiresAt.Time
	}
	mintAdminToken = func() (string, string, time.Time) {
		return mintFn("user:admin@example.com", []string{"admin"})
	}
	mintUserToken = func() (string, string, time.Time) {
		return mintFn("user:viewer@example.com", []string{"viewer"})
	}

	store := auth.NewPGRevocationStore(pg.Pool)
	checker := auth.NewCachedRevocationChecker(store, 30*time.Second)

	router = chi.NewRouter()
	router.Use(auth.MiddlewareFullWithRevocation(signer, nil, nil, nil, nil, checker))
	router.With(auth.RequirePermission(auth.PermUserManage)).
		Method(http.MethodPost, "/api/auth/tokens/{jti}/revoke",
			NewAdminAuthRevokeHandler(AdminAuthRevokeDeps{Store: store, Checker: checker}))

	router.Get("/protected", func(w http.ResponseWriter, r *http.Request) {
		u := auth.UserFromContext(r.Context())
		if u == nil {
			http.Error(w, "no user", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("ok:" + u.ID))
	})

	return router, pg, signer, mintAdminToken, mintUserToken
}

func countRevokedRowsBDD(t *testing.T, pg *testutil.PGContainer, jti string) int64 {
	t.Helper()
	var n int64
	err := pg.Pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM auth_revoked_tokens WHERE jti = $1`, jti).Scan(&n)
	if err != nil {
		t.Fatalf("count auth_revoked_tokens: %v", err)
	}
	return n
}

func TestBDD_US491_Given_RevokedToken_When_RequestReplays_Then_401(t *testing.T) {
	router, pg, _, mintAdminToken, _ := setupUS491BDDFixture(t)

	adminTok, adminJTI, adminExp := mintAdminToken()
	if adminJTI == "" {
		t.Fatal("admin jti must be non-empty")
	}

	// --- Given: baseline that the token currently passes /protected.
	{
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+adminTok)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("baseline /protected: got %d body=%s", rec.Code, rec.Body.String())
		}
	}

	// --- When: admin revokes its own jti.
	body := mustJSON(t, map[string]string{
		"userId":    "user:admin@example.com",
		"reason":    "self-logout",
		"expiresAt": adminExp.UTC().Format(time.RFC3339),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/tokens/"+adminJTI+"/revoke", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke failed: got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp AdminAuthRevokeResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode revoke body: %v", err)
	}
	if resp.JTI != adminJTI {
		t.Errorf("revoke body jti: got %q want %q", resp.JTI, adminJTI)
	}

	// --- Then: raw SQL proves the row landed.
	if n := countRevokedRowsBDD(t, pg, adminJTI); n != 1 {
		t.Fatalf("auth_revoked_tokens count for %q: got %d want 1", adminJTI, n)
	}

	// --- Then: the SAME token replayed must now be rejected with 401.
	{
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+adminTok)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("post-revoke /protected: got %d body=%s; want 401 TokenRevoked",
				rec.Code, rec.Body.String())
		}
	}
}

func TestBDD_US491_Given_NonRevokedToken_When_Request_Then_StillAdmitted(t *testing.T) {
	router, _, _, mintAdminToken, _ := setupUS491BDDFixture(t)
	// Mint a NEW admin token. It shares the signer with the fixture but has
	// a fresh jti so the revocation store has no row for it.
	adminTok, _, _ := mintAdminToken()

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("non-revoked /protected: got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestBDD_US491_Given_NonAdminToken_When_Revoke_Then_403_NoRow(t *testing.T) {
	router, pg, _, _, mintUserToken := setupUS491BDDFixture(t)
	viewerTok, viewerJTI, _ := mintUserToken()

	body := mustJSON(t, map[string]string{"reason": "should be rejected"})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/tokens/"+viewerJTI+"/revoke", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+viewerTok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin revoke: got %d body=%s; want 403", rec.Code, rec.Body.String())
	}
	if n := countRevokedRowsBDD(t, pg, viewerJTI); n != 0 {
		t.Errorf("auth_revoked_tokens count for rejected revoke: got %d want 0", n)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	return out
}
