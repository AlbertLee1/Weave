package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestSignerWithTTL(t *testing.T, ttl time.Duration) *JWTSigner {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: ttl,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestJWTMode_ValidTokenAllows(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, 15*time.Minute)

	tok, err := signer.Sign(SignInput{
		UserID:        "user:alice@example.com",
		Email:         "alice@example.com",
		Name:          "Alice",
		Roles:         []string{"editor"},
		OntologyRoles: map[string]string{"ri.ontology.main.ontology.northwind": "ontology-owner"},
	})
	if err != nil {
		t.Fatal(err)
	}

	mw := Middleware(signer)
	srv := mw(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rec.Code, rec.Body.String())
	}
	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatal(err)
	}
	if u.ID != "user:alice@example.com" {
		t.Errorf("ID: got %q", u.ID)
	}
	if u.Email != "alice@example.com" {
		t.Errorf("Email: got %q", u.Email)
	}
	if len(u.Roles) != 1 || u.Roles[0] != "editor" {
		t.Errorf("Roles: got %v", u.Roles)
	}
	if u.OntologyRoles["ri.ontology.main.ontology.northwind"] != "ontology-owner" {
		t.Errorf("OntologyRoles: got %v", u.OntologyRoles)
	}
}

// TestJWTMode_InjectsMarkingsAttribute verifies US-054: when a JWT carries a
// markings claim, the middleware must inject the slice into
// user.Attributes[MarkingsAttributeKey] so downstream policy / handler code
// can read it uniformly via auth.Markings(ctx).
func TestJWTMode_InjectsMarkingsAttribute(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, 15*time.Minute)

	tok, err := signer.Sign(SignInput{
		UserID:   "user:alice@example.com",
		Markings: []string{"PUBLIC", "PII"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var captured *User
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = UserFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	mw := Middleware(signer)
	srv := mw(h)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	srv.ServeHTTP(httptest.NewRecorder(), req)

	if captured == nil {
		t.Fatal("expected user in context")
	}
	got := Markings(contextWithUser(captured))
	if len(got) != 2 || got[0] != "PUBLIC" || got[1] != "PII" {
		t.Errorf("Markings: got %v, want [PUBLIC PII]", got)
	}
}

// contextWithUser is a tiny helper that wraps a captured user so Markings()
// can read from context exactly like a real handler would.
func contextWithUser(u *User) context.Context {
	return WithUser(context.Background(), u)
}

func TestJWTMode_MissingHeader(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, 15*time.Minute)

	mw := Middleware(signer)
	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestJWTMode_ExpiredToken(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, -1*time.Minute)
	tok, err := signer.Sign(SignInput{UserID: "user:alice"})
	if err != nil {
		t.Fatal(err)
	}

	mw := Middleware(signer)
	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 expired, got %d", rec.Code)
	}
}

func TestJWTMode_TamperedToken(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	signer := newTestSignerWithTTL(t, 15*time.Minute)
	tok, _ := signer.Sign(SignInput{UserID: "user:alice"})

	bs := []byte(tok)
	// Ensure the byte actually changes (avoid no-op when it is already 'A').
	if bs[len(bs)/2] == 'A' {
		bs[len(bs)/2] = 'B'
	} else {
		bs[len(bs)/2] = 'A'
	}
	mw := Middleware(signer)
	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+string(bs))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 tampered, got %d", rec.Code)
	}
}

func TestJWTMode_MissingSignerErrors(t *testing.T) {
	t.Setenv("AUTH_MODE", "jwt")
	mw := Middleware() // no signer
	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer whatever")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 misconfigured, got %d", rec.Code)
	}
}

func TestDevMode_StillWorksWithSignerArg(t *testing.T) {
	t.Setenv("AUTH_MODE", "dev")
	signer := newTestSignerWithTTL(t, time.Minute)

	mw := Middleware(signer)
	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected dev mode 200, got %d", rec.Code)
	}
}

func TestTokenMode_StillWorksWithSignerArg(t *testing.T) {
	t.Setenv("AUTH_MODE", "token")
	signer := newTestSignerWithTTL(t, time.Minute)

	mw := Middleware(signer)
	srv := mw(handler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected token mode 200, got %d", rec.Code)
	}
	var u User
	json.NewDecoder(rec.Body).Decode(&u)
	if u.ID != "my-secret-token" {
		t.Errorf("ID: got %q", u.ID)
	}
}
