package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// US-491: middleware must reject access tokens whose JTI is on the
// revocation blacklist, even when the token is otherwise valid (signature
// + exp + iss + aud all check out). The check happens after JWT.Verify so
// a malformed token still returns the canonical 401 InvalidSignature.

func TestUS491_JWTMode_RevokedToken_Returns401(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, 15*time.Minute)
	tok, err := signer.Sign(SignInput{UserID: "user:alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Verify(tok)
	if err != nil {
		t.Fatal(err)
	}

	store := NewMemoryRevocationStore()
	if err := store.Revoke(context.Background(), RevocationRecord{
		JTI:       claims.ID,
		UserID:    claims.Subject,
		ExpiresAt: claims.ExpiresAt.Time,
		Reason:    "test-revoke",
	}); err != nil {
		t.Fatal(err)
	}
	checker := NewCachedRevocationChecker(store, time.Minute)

	mw := MiddlewareWithRevocation(signer, nil, nil, nil, checker)
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for revoked token, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUS491_JWTMode_NonRevokedToken_StillAuthenticates(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, 15*time.Minute)
	tok, err := signer.Sign(SignInput{UserID: "user:bob@example.com", Roles: []string{"viewer"}})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemoryRevocationStore()
	checker := NewCachedRevocationChecker(store, time.Minute)

	mw := MiddlewareWithRevocation(signer, nil, nil, nil, checker)
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for non-revoked token, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUS491_JWTMode_NilChecker_NoOp(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, 15*time.Minute)
	tok, err := signer.Sign(SignInput{UserID: "user:degraded@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	// Use the legacy non-revocation constructor: still must allow.
	mw := Middleware(signer)
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no revocation checker (degraded boot), got %d", rec.Code)
	}
}
