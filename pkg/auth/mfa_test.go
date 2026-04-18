package auth

import (
	"errors"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestGenerateTOTPSecret_Valid(t *testing.T) {
	key, err := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if key.Secret() == "" {
		t.Error("expected non-empty secret")
	}
	if key.Issuer() != "Weave-Test" {
		t.Errorf("issuer: got %q, want Weave-Test", key.Issuer())
	}
	if key.AccountName() != "alice@example.com" {
		t.Errorf("account: got %q, want alice@example.com", key.AccountName())
	}
}

func TestGenerateTOTPSecret_DefaultIssuer(t *testing.T) {
	key, err := GenerateTOTPSecret("", "bob@example.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if key.Issuer() != DefaultMFAIssuer {
		t.Errorf("issuer: got %q, want %q", key.Issuer(), DefaultMFAIssuer)
	}
}

func TestGenerateTOTPSecret_EmptyAccountRejected(t *testing.T) {
	if _, err := GenerateTOTPSecret("Weave", ""); err == nil {
		t.Error("expected error for empty account")
	}
}

func TestValidateTOTPCode_HappyPath(t *testing.T) {
	key, err := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	now := time.Now()
	code, err := totp.GenerateCode(key.Secret(), now)
	if err != nil {
		t.Fatalf("code gen: %v", err)
	}
	if err := ValidateTOTPCode(key.Secret(), code, now); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func TestValidateTOTPCode_RejectsBadCode(t *testing.T) {
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	err := ValidateTOTPCode(key.Secret(), "000000", time.Now())
	if err == nil {
		t.Fatal("expected validation failure")
	}
	if !errors.Is(err, ErrInvalidMFACode) {
		t.Errorf("expected ErrInvalidMFACode, got %v", err)
	}
}

func TestValidateTOTPCode_AcceptsSkewWithin30s(t *testing.T) {
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	t0 := time.Now()
	codeT0, _ := totp.GenerateCode(key.Secret(), t0)
	// 30s later the same code from t0 should still validate (skew=1 step).
	if err := ValidateTOTPCode(key.Secret(), codeT0, t0.Add(30*time.Second)); err != nil {
		t.Errorf("expected skew-tolerant validation, got %v", err)
	}
}

func TestValidateTOTPCode_RejectsLargeSkew(t *testing.T) {
	key, _ := GenerateTOTPSecret("Weave-Test", "alice@example.com")
	t0 := time.Now()
	codeT0, _ := totp.GenerateCode(key.Secret(), t0)
	// 5 minutes later the t0 code is far past the skew window.
	if err := ValidateTOTPCode(key.Secret(), codeT0, t0.Add(5*time.Minute)); err == nil {
		t.Error("expected rejection for >1-step skew")
	}
}

func TestValidateTOTPCode_RejectsEmptyInputs(t *testing.T) {
	if err := ValidateTOTPCode("", "123456", time.Now()); err == nil {
		t.Error("expected rejection for empty secret")
	}
	if err := ValidateTOTPCode("JBSWY3DPEHPK3PXP", "", time.Now()); err == nil {
		t.Error("expected rejection for empty code")
	}
}

func TestMFAChallengeStore_IssueConsumeRoundTrip(t *testing.T) {
	s := NewMFAChallengeStore(time.Minute)
	tok, err := s.Issue("user:alice@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if tok == "" {
		t.Fatal("expected non-empty token")
	}
	uid, err := s.Consume(tok)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if uid != "user:alice@example.com" {
		t.Errorf("user id: got %q", uid)
	}
}

func TestMFAChallengeStore_SingleUse(t *testing.T) {
	s := NewMFAChallengeStore(time.Minute)
	tok, _ := s.Issue("alice")
	if _, err := s.Consume(tok); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	_, err := s.Consume(tok)
	if !errors.Is(err, ErrMFAChallengeNotFound) {
		t.Errorf("expected ErrMFAChallengeNotFound on replay, got %v", err)
	}
}

func TestMFAChallengeStore_TTLEviction(t *testing.T) {
	s := NewMFAChallengeStore(50 * time.Millisecond)
	clock := time.Now()
	s.SetNowFunc(func() time.Time { return clock })

	tok, _ := s.Issue("alice")
	clock = clock.Add(100 * time.Millisecond)
	_, err := s.Consume(tok)
	if !errors.Is(err, ErrMFAChallengeExpired) {
		t.Errorf("expected ErrMFAChallengeExpired, got %v", err)
	}
	if got := s.Size(); got != 0 {
		t.Errorf("expected gc to evict expired entry, size=%d", got)
	}
}

func TestMFAChallengeStore_UnknownToken(t *testing.T) {
	s := NewMFAChallengeStore(time.Minute)
	_, err := s.Consume("nope")
	if !errors.Is(err, ErrMFAChallengeNotFound) {
		t.Errorf("expected ErrMFAChallengeNotFound, got %v", err)
	}
}

func TestMFAChallengeStore_EmptyTokenRejected(t *testing.T) {
	s := NewMFAChallengeStore(time.Minute)
	_, err := s.Consume("")
	if !errors.Is(err, ErrMFAChallengeNotFound) {
		t.Errorf("expected ErrMFAChallengeNotFound for empty token, got %v", err)
	}
}

func TestMFAChallengeStore_EmptyUserIDRejected(t *testing.T) {
	s := NewMFAChallengeStore(time.Minute)
	if _, err := s.Issue(""); err == nil {
		t.Error("expected error for empty userID")
	}
}

func TestMFAChallengeStore_DefaultTTL(t *testing.T) {
	s := NewMFAChallengeStore(0)
	if s.ttl != DefaultMFAChallengeTTL {
		t.Errorf("ttl: got %v, want %v", s.ttl, DefaultMFAChallengeTTL)
	}
}
