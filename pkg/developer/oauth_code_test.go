package developer

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestComputePKCEChallenge_MatchesRFC7636Example(t *testing.T) {
	// Example verifier + challenge from RFC 7636 §4.3.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	got := ComputePKCEChallenge(verifier)
	if got != want {
		t.Errorf("ComputePKCEChallenge: got %q, want %q", got, want)
	}
}

func TestVerifyPKCE_Success(t *testing.T) {
	verifier := strings.Repeat("a", 43)
	challenge := ComputePKCEChallenge(verifier)
	if err := VerifyPKCE(challenge, verifier, "S256"); err != nil {
		t.Errorf("VerifyPKCE on matching pair: %v", err)
	}
	// Empty method defaults to S256.
	if err := VerifyPKCE(challenge, verifier, ""); err != nil {
		t.Errorf("VerifyPKCE default method: %v", err)
	}
}

func TestVerifyPKCE_Mismatch(t *testing.T) {
	verifier := strings.Repeat("a", 43)
	otherVerifier := strings.Repeat("b", 43)
	challenge := ComputePKCEChallenge(otherVerifier)
	err := VerifyPKCE(challenge, verifier, "S256")
	if !errors.Is(err, ErrPKCEChallengeMismatch) {
		t.Errorf("expected ErrPKCEChallengeMismatch, got %v", err)
	}
}

func TestVerifyPKCE_RejectsPlainMethod(t *testing.T) {
	verifier := strings.Repeat("a", 43)
	// plain method: code_challenge == code_verifier
	err := VerifyPKCE(verifier, verifier, "plain")
	if !errors.Is(err, ErrUnsupportedPKCEMethod) {
		t.Errorf("expected ErrUnsupportedPKCEMethod, got %v", err)
	}
}

func TestVerifyPKCE_RejectsShortVerifier(t *testing.T) {
	verifier := "too-short"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	err := VerifyPKCE(challenge, verifier, "S256")
	if !errors.Is(err, ErrInvalidPKCEVerifier) {
		t.Errorf("expected ErrInvalidPKCEVerifier on short input, got %v", err)
	}
}

func TestVerifyPKCE_RejectsDisallowedChars(t *testing.T) {
	// 43 chars but contains a space
	verifier := strings.Repeat("a", 42) + " "
	challenge := ComputePKCEChallenge(verifier)
	err := VerifyPKCE(challenge, verifier, "S256")
	if !errors.Is(err, ErrInvalidPKCEVerifier) {
		t.Errorf("expected ErrInvalidPKCEVerifier on space char, got %v", err)
	}
}

func TestGenerateAuthorizationCode_UniqueAndPrefixed(t *testing.T) {
	c1, err := GenerateAuthorizationCode()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	c2, err := GenerateAuthorizationCode()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if c1 == c2 {
		t.Errorf("expected two distinct codes, got duplicate %q", c1)
	}
	if !strings.HasPrefix(c1, AuthCodePrefix) {
		t.Errorf("missing prefix: %q", c1)
	}
}

func TestAuthorizationCode_IsUsable(t *testing.T) {
	now := time.Now()
	t.Run("fresh", func(t *testing.T) {
		c := &AuthorizationCode{ExpiresAt: now.Add(5 * time.Minute)}
		if err := c.IsUsable(now); err != nil {
			t.Errorf("expected usable, got %v", err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		c := &AuthorizationCode{ExpiresAt: now.Add(-1 * time.Minute)}
		if err := c.IsUsable(now); !errors.Is(err, ErrAuthorizationCodeExpired) {
			t.Errorf("expected expired, got %v", err)
		}
	})
	t.Run("consumed", func(t *testing.T) {
		stamp := now.Add(-1 * time.Second)
		c := &AuthorizationCode{ExpiresAt: now.Add(5 * time.Minute), ConsumedAt: &stamp}
		if err := c.IsUsable(now); !errors.Is(err, ErrAuthorizationCodeConsumed) {
			t.Errorf("expected consumed, got %v", err)
		}
	})
}

func TestValidateRedirectURI(t *testing.T) {
	app := &Application{RedirectURIs: []string{"https://a.example.com/cb", "https://b.example.com/cb"}}
	if err := ValidateRedirectURI(app, "https://a.example.com/cb"); err != nil {
		t.Errorf("exact match: %v", err)
	}
	if err := ValidateRedirectURI(app, "https://evil.example.com/cb"); !errors.Is(err, ErrInvalidRedirectURI) {
		t.Errorf("unknown uri: expected ErrInvalidRedirectURI, got %v", err)
	}
	// Trailing-slash difference is rejected (exact match).
	if err := ValidateRedirectURI(app, "https://a.example.com/cb/"); !errors.Is(err, ErrInvalidRedirectURI) {
		t.Errorf("trailing slash: expected ErrInvalidRedirectURI, got %v", err)
	}
}
