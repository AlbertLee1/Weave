//go:build integration

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/liyang/weave/pkg/auth"
)

// US-490 — JWT 多密钥轮换 (BDD).
//
// PRD 验收：
//   - JWTSigner 持有 key ring（oldest verifies, newest signs）
//   - POST /api/admin/auth/keys/rotate
//   - 测试：旧 token 仍校验 + 新 token 用新 key
//
// 这条 BDD 走的是完整的 wire surface:
//   - chi router + auth.Middleware(signer) + auth.RequirePermission(PermUserManage)
//   - 真 RSA 签名 / 真 JWT 验证（不 mock keyfunc）
//   - 真 NewAdminAuthKeysRotateHandler — 调用同一个 signer 实例
//
// Scenario A (happy path):
//   Given a server with one seeded JWT signing key and a JWT minted under it
//   When an admin token rotates the keyring via POST /api/admin/auth/keys/rotate
//   Then the response carries a new activeKeyId differing from the seed kid
//    And the pre-rotate token still verifies against the same /protected echo
//    And a freshly minted token's JOSE header carries the new kid
//    And the new token also passes /protected echo
//
// Scenario B (auth gate negative control):
//   Given a JWT minted for a non-admin user
//   When the user POSTs /api/admin/auth/keys/rotate
//   Then the request is rejected with 403 PermissionDenied
//    And the keyring is unchanged (no silent rotation on permission failure)
//
// Scenario C (auth gate missing-token control):
//   Given no Authorization header
//   When the request hits POST /api/admin/auth/keys/rotate
//   Then the request is rejected with 401 MissingAuthorization
//    And the keyring is unchanged.

func setupUS490BDDFixture(t *testing.T) (router *chi.Mux, signer *auth.JWTSigner, mintAdminToken func() string, mintUserToken func() string) {
	t.Helper()
	t.Setenv("AUTH_MODE", "jwt")

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

	mintAdminToken = func() string {
		tok, err := signer.Sign(auth.SignInput{
			UserID: "user:admin@example.com",
			Email:  "admin@example.com",
			Name:   "Admin",
			// admin role grants PermUserManage; see pkg/auth/permissions.go.
			Roles: []string{"admin"},
		})
		if err != nil {
			t.Fatalf("Sign admin: %v", err)
		}
		return tok
	}
	mintUserToken = func() string {
		tok, err := signer.Sign(auth.SignInput{
			UserID: "user:viewer@example.com",
			Email:  "viewer@example.com",
			Name:   "Viewer",
			Roles:  []string{"viewer"},
		})
		if err != nil {
			t.Fatalf("Sign viewer: %v", err)
		}
		return tok
	}

	router = chi.NewRouter()
	router.Use(auth.Middleware(signer))
	router.With(auth.RequirePermission(auth.PermUserManage)).
		Method(http.MethodPost, "/api/admin/auth/keys/rotate",
			NewAdminAuthKeysRotateHandler(AdminAuthKeysRotateDeps{Signer: signer}))

	// /protected is a no-op echo gated only by the JWT middleware. It lets
	// the BDD assert "this token verifies" without depending on any business
	// resource — the moment auth.Middleware lets it through, we know the
	// keyring has admitted the token.
	router.Get("/protected", func(w http.ResponseWriter, r *http.Request) {
		u := auth.UserFromContext(r.Context())
		if u == nil {
			http.Error(w, "no user", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("ok:" + u.ID))
	})

	return router, signer, mintAdminToken, mintUserToken
}

func TestBDD_US490_Given_RingRotated_When_OldAndNewTokensUsed_Then_BothVerify(t *testing.T) {
	router, signer, mintAdminToken, _ := setupUS490BDDFixture(t)
	ctx := context.Background()
	_ = ctx

	priorKid := signer.ActiveKeyID()
	if priorKid == "" {
		t.Fatal("seed signer must have an active kid")
	}

	// --- Given: a token minted before any rotate.
	oldAdminTok := mintAdminToken()

	// --- Given: confirm the old token verifies on the protected echo
	// (forms the baseline so the post-rotate assertion is meaningful).
	{
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+oldAdminTok)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("baseline /protected with old token: got %d body=%s", rec.Code, rec.Body.String())
		}
	}

	// --- When: admin rotates the keyring via the wire endpoint.
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/keys/rotate", nil)
	req.Header.Set("Authorization", "Bearer "+oldAdminTok)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate failed: got %d body=%s", rec.Code, rec.Body.String())
	}
	var rotateResp AdminAuthKeysRotateResponse
	if err := json.NewDecoder(rec.Body).Decode(&rotateResp); err != nil {
		t.Fatalf("decode rotate body: %v", err)
	}
	if rotateResp.ActiveKeyId == priorKid {
		t.Errorf("activeKeyId must differ from seed kid; got %q == %q", rotateResp.ActiveKeyId, priorKid)
	}
	if len(rotateResp.KeyIds) != 2 || rotateResp.KeyIds[0] != priorKid || rotateResp.KeyIds[1] != rotateResp.ActiveKeyId {
		t.Errorf("keyIds expected [%q,%q]; got %v", priorKid, rotateResp.ActiveKeyId, rotateResp.KeyIds)
	}

	// --- Then: the pre-rotate token MUST still verify on /protected.
	{
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+oldAdminTok)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("post-rotate verification of old token failed: got %d body=%s", rec.Code, rec.Body.String())
		}
	}

	// --- Then: a fresh token is signed under the new kid AND verifies.
	newAdminTok := mintAdminToken()
	{
		// Inspect the new token's JOSE header — it must carry the new kid.
		parser := jwt.NewParser()
		parsed, _, err := parser.ParseUnverified(newAdminTok, jwt.MapClaims{})
		if err != nil {
			t.Fatalf("ParseUnverified new token: %v", err)
		}
		gotKid, _ := parsed.Header["kid"].(string)
		if gotKid != rotateResp.ActiveKeyId {
			t.Errorf("new token kid header: got %q want %q", gotKid, rotateResp.ActiveKeyId)
		}
	}
	{
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+newAdminTok)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("new token /protected: got %d body=%s", rec.Code, rec.Body.String())
		}
		if got := rec.Body.String(); !strings.Contains(got, "user:admin@example.com") {
			t.Errorf("echo body: got %q want user:admin@example.com", got)
		}
	}
}

func TestBDD_US490_Given_NonAdminCaller_When_RotateRequested_Then_Rejected_403_AndKeyringUnchanged(t *testing.T) {
	router, signer, _, mintUserToken := setupUS490BDDFixture(t)

	preRotateKids := signer.KeyIDs()
	if len(preRotateKids) != 1 {
		t.Fatalf("expected 1 seed key; got %v", preRotateKids)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/keys/rotate", nil)
	req.Header.Set("Authorization", "Bearer "+mintUserToken())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin must get 403; got %d body=%s", rec.Code, rec.Body.String())
	}

	// Key state must not change on permission failure — silent rotation
	// would defeat the audit trail. Read it after the call to assert.
	postRotateKids := signer.KeyIDs()
	if len(postRotateKids) != 1 || postRotateKids[0] != preRotateKids[0] {
		t.Errorf("keyring must be unchanged on 403; pre=%v post=%v", preRotateKids, postRotateKids)
	}
}

func TestBDD_US490_Given_NoAuthHeader_When_RotateRequested_Then_Rejected_401_AndKeyringUnchanged(t *testing.T) {
	router, signer, _, _ := setupUS490BDDFixture(t)

	preRotateKids := signer.KeyIDs()

	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/keys/rotate", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token must get 401; got %d body=%s", rec.Code, rec.Body.String())
	}

	postRotateKids := signer.KeyIDs()
	if len(postRotateKids) != len(preRotateKids) {
		t.Errorf("keyring must be unchanged on 401; pre=%v post=%v", preRotateKids, postRotateKids)
	}
}
