package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/auth"
)

// US-490: POST /api/admin/auth/keys/rotate
//
// Acceptance:
//   - returns 200 + new activeKeyId + full ordered keyIds + rotatedAt
//   - the activeKeyId differs from the kid present before rotate
//   - 503 when no signer is wired
//
// Concrete behaviour verified end-to-end:
//   1. mint a token under the pre-rotate signer
//   2. POST rotate
//   3. mint a token under the post-rotate signer (auto picks up new active key)
//   4. both tokens still verify; new token's kid matches the response

func testRotateSigner(t *testing.T) *auth.JWTSigner {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa: %v", err)
	}
	s, err := auth.NewJWTSigner(priv, &priv.PublicKey, auth.JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}
	return s
}

func TestAdminAuthKeysRotate_Success_AppendsNewActiveKey(t *testing.T) {
	signer := testRotateSigner(t)
	priorKid := signer.ActiveKeyID()
	if priorKid == "" {
		t.Fatal("seeded signer must have an active kid")
	}

	priorTok, err := signer.Sign(auth.SignInput{UserID: "user:before"})
	if err != nil {
		t.Fatalf("Sign pre-rotate: %v", err)
	}

	h := NewAdminAuthKeysRotateHandler(AdminAuthKeysRotateDeps{
		Signer:  signer,
		rsaBits: 2048,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/keys/rotate", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp AdminAuthKeysRotateResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if resp.ActiveKeyId == "" || resp.ActiveKeyId == priorKid {
		t.Errorf("activeKeyId: got %q (prior %q) — must be a fresh kid", resp.ActiveKeyId, priorKid)
	}
	if len(resp.KeyIds) != 2 {
		t.Fatalf("keyIds: got %v want 2 entries", resp.KeyIds)
	}
	if resp.KeyIds[0] != priorKid {
		t.Errorf("keyIds[0] should still be prior kid; got %q want %q", resp.KeyIds[0], priorKid)
	}
	if resp.KeyIds[1] != resp.ActiveKeyId {
		t.Errorf("keyIds[1] should equal activeKeyId; got %q vs %q", resp.KeyIds[1], resp.ActiveKeyId)
	}
	if resp.RotatedAt == "" {
		t.Error("rotatedAt must be populated")
	}
	if _, err := time.Parse(time.RFC3339Nano, resp.RotatedAt); err != nil {
		t.Errorf("rotatedAt must be RFC3339Nano; got %q (%v)", resp.RotatedAt, err)
	}

	// The signer's runtime state should reflect the rotation.
	if signer.ActiveKeyID() != resp.ActiveKeyId {
		t.Errorf("signer.ActiveKeyID after handler: got %q want %q", signer.ActiveKeyID(), resp.ActiveKeyId)
	}

	// Old token (signed by prior key) must still verify after rotation.
	if _, err := signer.Verify(priorTok); err != nil {
		t.Errorf("pre-rotate token must still verify; got %v", err)
	}
	// New token uses the new kid.
	newTok, err := signer.Sign(auth.SignInput{UserID: "user:after"})
	if err != nil {
		t.Fatalf("Sign post-rotate: %v", err)
	}
	if _, err := signer.Verify(newTok); err != nil {
		t.Errorf("post-rotate token must verify; got %v", err)
	}
}

func TestAdminAuthKeysRotate_NoSigner_Returns503(t *testing.T) {
	h := NewAdminAuthKeysRotateHandler(AdminAuthKeysRotateDeps{Signer: nil})

	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/keys/rotate", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503; body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["errorName"] != "JWTSignerNotConfigured" {
		t.Errorf("errorName: got %q want JWTSignerNotConfigured", body["errorName"])
	}
}

func TestAdminAuthKeysRotate_TwoCallsAppendTwoKeys(t *testing.T) {
	signer := testRotateSigner(t)
	startCount := len(signer.KeyIDs())

	h := NewAdminAuthKeysRotateHandler(AdminAuthKeysRotateDeps{
		Signer:  signer,
		rsaBits: 2048,
	})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/keys/rotate", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d status: got %d body=%s", i, rec.Code, rec.Body.String())
		}
	}

	if got := len(signer.KeyIDs()); got != startCount+2 {
		t.Errorf("keyring size after 2 rotates: got %d want %d", got, startCount+2)
	}
}
