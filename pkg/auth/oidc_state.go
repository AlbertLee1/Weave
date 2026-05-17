// Package auth — OIDC state HMAC signer (US-492).
//
// The OIDC Authorization-Code flow round-trips a `state` query parameter
// through the user-agent on a 302 redirect. RFC 6749 leaves the state opaque
// so callers historically used a random nonce + server-side cookie binding
// for CSRF defense (the legacy auth/oidc_handler.go path). US-492 hardens
// that contract: state itself becomes a tamper-evident, time-bounded blob
// signed with a server-only HMAC secret, so a stale or modified state is
// rejected before any cookie comparison runs. Cookie binding remains as a
// second layer of CSRF defense (replay-binding to the originating browser),
// but the primary anti-replay / anti-forgery defense is now the HMAC + 5min
// window.
//
// Layout (binary, then RawURL-base64-encoded into the state query param):
//
//	bytes[0:16]   nonce            (crypto/rand, defeats prefix-prediction)
//	bytes[16:24]  timestamp        (signed-unix big-endian int64)
//	bytes[24:40]  hmac_sha256[:16] (truncated tag of nonce||timestamp)
//
// Total: 40 raw bytes → 54 base64url chars. Well under URL length limits.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

const (
	stateNonceLen     = 16
	stateTimestampLen = 8
	stateMACLen       = 16
	statePayloadLen   = stateNonceLen + stateTimestampLen
	stateRawLen       = statePayloadLen + stateMACLen

	// stateMinSecretLen mirrors HMAC-SHA256's minimum effective input length;
	// shorter secrets are categorically rejected at construction time to head
	// off footgun configs that would otherwise just hash zeros.
	stateMinSecretLen = 16

	// DefaultStateTTL is the PRD-mandated 5-minute window beyond which a
	// signed state is rejected as Expired even though the HMAC still validates.
	DefaultStateTTL = 5 * time.Minute
)

// ErrStateInvalid signals an HMAC mismatch, truncated payload, malformed
// base64, or a future-dated state more than TTL ahead of now. Callers map
// this to a 401 OIDCStateInvalid response.
var ErrStateInvalid = errors.New("oidc state invalid")

// ErrStateExpired signals an HMAC-valid but age > TTL state. Callers map
// this to a 401 OIDCStateExpired response so SDK consumers can distinguish
// "I was attacked" from "the user took too long".
var ErrStateExpired = errors.New("oidc state expired")

// HMACStateSigner mints and verifies HMAC-signed OIDC state values. Safe
// for concurrent use after construction.
type HMACStateSigner struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
	rand   io.Reader
}

// NewHMACStateSigner constructs a signer. ttl<=0 falls back to DefaultStateTTL.
// secret shorter than stateMinSecretLen bytes is rejected — operators should
// pass at least 32 bytes from a cryptographically strong source (env var
// decoded from base64, file from disk, KMS handle, etc).
func NewHMACStateSigner(secret []byte, ttl time.Duration) (*HMACStateSigner, error) {
	if len(secret) < stateMinSecretLen {
		return nil, errors.New("oidc state secret must be at least 16 bytes")
	}
	if ttl <= 0 {
		ttl = DefaultStateTTL
	}
	cp := make([]byte, len(secret))
	copy(cp, secret)
	return &HMACStateSigner{
		secret: cp,
		ttl:    ttl,
		now:    time.Now,
		rand:   rand.Reader,
	}, nil
}

// TTL returns the configured validity window.
func (s *HMACStateSigner) TTL() time.Duration { return s.ttl }

// SetNow swaps the clock source — only used by tests to drive deterministic
// age assertions. Production callers must leave this alone.
func (s *HMACStateSigner) SetNow(fn func() time.Time) {
	if fn != nil {
		s.now = fn
	}
}

// Sign generates a random nonce, stamps the supplied timestamp, and returns
// a base64url-encoded HMAC-signed state value.
func (s *HMACStateSigner) Sign(issued time.Time) (string, error) {
	nonce := make([]byte, stateNonceLen)
	if _, err := io.ReadFull(s.rand, nonce); err != nil {
		return "", err
	}
	raw := make([]byte, stateRawLen)
	copy(raw[:stateNonceLen], nonce)
	binary.BigEndian.PutUint64(raw[stateNonceLen:statePayloadLen], uint64(issued.Unix()))
	mac := s.computeMAC(raw[:statePayloadLen])
	copy(raw[statePayloadLen:], mac)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Verify decodes the supplied state, recomputes the HMAC over its
// nonce||timestamp prefix, constant-time compares, and finally enforces the
// 5-minute age window. Returns ErrStateInvalid for any structural / signature
// failure and ErrStateExpired for an HMAC-valid but stale state.
func (s *HMACStateSigner) Verify(state string) error {
	if state == "" {
		return ErrStateInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(state)
	if err != nil || len(raw) != stateRawLen {
		return ErrStateInvalid
	}
	payload := raw[:statePayloadLen]
	tag := raw[statePayloadLen:]
	expected := s.computeMAC(payload)
	if subtle.ConstantTimeCompare(tag, expected) != 1 {
		return ErrStateInvalid
	}
	tsUnix := int64(binary.BigEndian.Uint64(payload[stateNonceLen:statePayloadLen]))
	issued := time.Unix(tsUnix, 0)
	now := s.now()
	age := now.Sub(issued)
	// Future-dated states beyond TTL are nonsense — treat as forged-clock.
	if age < -s.ttl {
		return ErrStateInvalid
	}
	if age > s.ttl {
		return ErrStateExpired
	}
	return nil
}

func (s *HMACStateSigner) computeMAC(payload []byte) []byte {
	h := hmac.New(sha256.New, s.secret)
	h.Write(payload)
	return h.Sum(nil)[:stateMACLen]
}
