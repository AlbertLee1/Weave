package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"
)

// newTestSigner creates a fresh RSA-2048 keypair signer for unit tests.
func newTestSigner(t *testing.T) *JWTSigner {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa key gen: %v", err)
	}
	s, err := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{
		Issuer:          "weave-test",
		Audience:        "weave-api",
		AccessTokenTTL:  15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}
	return s
}

func TestJWTSigner_SignVerifyRoundTrip(t *testing.T) {
	s := newTestSigner(t)

	tok, err := s.Sign(SignInput{
		UserID:        "user:alice@example.com",
		Email:         "alice@example.com",
		Name:          "Alice",
		Roles:         []string{"editor"},
		OntologyRoles: map[string]string{"ri.ontology.main.ontology.northwind": "ontology-owner"},
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user:alice@example.com" {
		t.Errorf("subject: got %q", claims.Subject)
	}
	if claims.Weave.Email != "alice@example.com" {
		t.Errorf("email: got %q", claims.Weave.Email)
	}
	if claims.Weave.Name != "Alice" {
		t.Errorf("name: got %q", claims.Weave.Name)
	}
	if len(claims.Weave.Roles) != 1 || claims.Weave.Roles[0] != "editor" {
		t.Errorf("roles: got %v", claims.Weave.Roles)
	}
	if claims.Weave.OntologyRoles["ri.ontology.main.ontology.northwind"] != "ontology-owner" {
		t.Errorf("ontologyRoles: got %v", claims.Weave.OntologyRoles)
	}
	if claims.Issuer != "weave-test" {
		t.Errorf("issuer: got %q", claims.Issuer)
	}
	if claims.Weave.Version != 1 {
		t.Errorf("version: got %d", claims.Weave.Version)
	}
}

func TestJWTSigner_ExpiredTokenRejected(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	s, err := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: -1 * time.Minute, // already expired
	})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := s.Sign(SignInput{UserID: "user:bob"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Verify(tok)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestJWTSigner_TamperedTokenRejected(t *testing.T) {
	s := newTestSigner(t)
	tok, err := s.Sign(SignInput{UserID: "user:alice"})
	if err != nil {
		t.Fatal(err)
	}

	// Tamper: flip a character in the middle of the token.
	bs := []byte(tok)
	bs[len(bs)/2] = 'A'
	tampered := string(bs)

	if _, err := s.Verify(tampered); err == nil {
		t.Error("expected verify to fail on tampered token")
	}
}

func TestJWTSigner_WrongKeyRejected(t *testing.T) {
	s1 := newTestSigner(t)
	s2 := newTestSigner(t)
	tok, err := s1.Sign(SignInput{UserID: "user:alice"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Verify(tok); err == nil {
		t.Error("expected verify with wrong key to fail")
	}
}

func TestJWTSigner_MissingPrivateKey(t *testing.T) {
	// Verifier-only signer (no private key) cannot sign but can verify.
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	verifierOnly, err := NewJWTSigner(nil, &priv.PublicKey, JWTSignerOptions{
		Issuer:         "weave-test",
		Audience:       "weave-api",
		AccessTokenTTL: 15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("NewJWTSigner verifier-only: %v", err)
	}
	if _, err := verifierOnly.Sign(SignInput{UserID: "user:alice"}); err == nil {
		t.Error("expected Sign to fail without private key")
	}
}

func TestJWTSigner_NilOptionsDefaults(t *testing.T) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	s, err := NewJWTSigner(priv, &priv.PublicKey, JWTSignerOptions{})
	if err != nil {
		t.Fatalf("NewJWTSigner: %v", err)
	}
	tok, err := s.Sign(SignInput{UserID: "user:alice"})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	claims, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Issuer != "weave" {
		t.Errorf("default issuer: got %q", claims.Issuer)
	}
}
