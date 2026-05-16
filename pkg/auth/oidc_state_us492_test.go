// US-492: OIDC state HMAC + 5min time-window verification.
//
// The state value travels across the user-agent on an untrusted redirect, so
// the IdP-returned `state` query param must be self-validating: any byte-level
// tamper, replay past the 5-minute window, or different-secret forgery must
// be rejected without trusting any server-side cookie.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func newTestStateSigner(t *testing.T) *HMACStateSigner {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("rand: %v", err)
	}
	signer, err := NewHMACStateSigner(secret, 5*time.Minute)
	if err != nil {
		t.Fatalf("NewHMACStateSigner: %v", err)
	}
	return signer
}

func TestHMACStateSigner_SignVerify_RoundTrip(t *testing.T) {
	signer := newTestStateSigner(t)
	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)

	state, err := signer.Sign(now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if state == "" {
		t.Fatal("Sign returned empty state")
	}

	signer.SetNow(func() time.Time { return now.Add(2 * time.Minute) })
	if err := signer.Verify(state); err != nil {
		t.Fatalf("Verify within window: %v", err)
	}
}

func TestHMACStateSigner_DistinctNoncesEachCall(t *testing.T) {
	signer := newTestStateSigner(t)
	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)

	a, _ := signer.Sign(now)
	b, _ := signer.Sign(now)
	if a == b {
		t.Fatalf("Sign produced identical states %q == %q (nonce not random)", a, b)
	}
}

func TestHMACStateSigner_RejectsTamperedPayload(t *testing.T) {
	signer := newTestStateSigner(t)
	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	state, err := signer.Sign(now)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Decode, flip a byte in the payload (nonce / timestamp region), re-encode.
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(raw) < 4 {
		t.Fatalf("state too short: %d", len(raw))
	}
	raw[0] ^= 0xFF
	tampered := base64.RawURLEncoding.EncodeToString(raw)

	signer.SetNow(func() time.Time { return now.Add(1 * time.Minute) })
	err = signer.Verify(tampered)
	if !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("tampered payload: got %v, want ErrStateInvalid", err)
	}
}

func TestHMACStateSigner_RejectsTamperedMAC(t *testing.T) {
	signer := newTestStateSigner(t)
	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	state, _ := signer.Sign(now)

	raw, _ := base64.RawURLEncoding.DecodeString(state)
	raw[len(raw)-1] ^= 0x01 // flip last MAC byte
	tampered := base64.RawURLEncoding.EncodeToString(raw)

	signer.SetNow(func() time.Time { return now.Add(1 * time.Minute) })
	if err := signer.Verify(tampered); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("tampered MAC: got %v, want ErrStateInvalid", err)
	}
}

func TestHMACStateSigner_RejectsExpiredState(t *testing.T) {
	signer := newTestStateSigner(t)
	signed := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	state, _ := signer.Sign(signed)

	// Just past the 5-minute window.
	signer.SetNow(func() time.Time { return signed.Add(5*time.Minute + time.Second) })
	if err := signer.Verify(state); !errors.Is(err, ErrStateExpired) {
		t.Fatalf("expired state: got %v, want ErrStateExpired", err)
	}
}

func TestHMACStateSigner_BoundaryAtTTL_StillValid(t *testing.T) {
	signer := newTestStateSigner(t)
	signed := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	state, _ := signer.Sign(signed)

	// Exactly at TTL boundary should still pass (== window upper bound is inclusive).
	signer.SetNow(func() time.Time { return signed.Add(5 * time.Minute) })
	if err := signer.Verify(state); err != nil {
		t.Fatalf("at-boundary verify: %v", err)
	}
}

func TestHMACStateSigner_RejectsForgeryWithDifferentSecret(t *testing.T) {
	now := time.Date(2026, 5, 17, 10, 0, 0, 0, time.UTC)
	attacker, _ := NewHMACStateSigner([]byte("attackeralwayswins12345"), 5*time.Minute)
	state, _ := attacker.Sign(now)

	victim := newTestStateSigner(t)
	victim.SetNow(func() time.Time { return now.Add(1 * time.Minute) })
	if err := victim.Verify(state); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("cross-secret forgery: got %v, want ErrStateInvalid", err)
	}
}

func TestHMACStateSigner_RejectsGarbledBase64(t *testing.T) {
	signer := newTestStateSigner(t)
	if err := signer.Verify("not-a-base64$$$"); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("garbled base64: got %v, want ErrStateInvalid", err)
	}
}

func TestHMACStateSigner_RejectsEmptyState(t *testing.T) {
	signer := newTestStateSigner(t)
	if err := signer.Verify(""); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("empty state: got %v, want ErrStateInvalid", err)
	}
}

func TestHMACStateSigner_RejectsShortPayload(t *testing.T) {
	signer := newTestStateSigner(t)
	// Valid base64 but too few bytes.
	short := base64.RawURLEncoding.EncodeToString([]byte("short"))
	if err := signer.Verify(short); !errors.Is(err, ErrStateInvalid) {
		t.Fatalf("short payload: got %v, want ErrStateInvalid", err)
	}
}

func TestNewHMACStateSigner_RejectsShortSecret(t *testing.T) {
	_, err := NewHMACStateSigner([]byte("short"), 5*time.Minute)
	if err == nil {
		t.Fatal("expected error for short secret")
	}
	if !strings.Contains(err.Error(), "secret") {
		t.Fatalf("error should mention secret: %v", err)
	}
}

func TestNewHMACStateSigner_DefaultsTTL(t *testing.T) {
	secret := make([]byte, 32)
	signer, err := NewHMACStateSigner(secret, 0)
	if err != nil {
		t.Fatalf("NewHMACStateSigner: %v", err)
	}
	if signer.TTL() != DefaultStateTTL {
		t.Fatalf("default TTL=%v, want %v", signer.TTL(), DefaultStateTTL)
	}
}
