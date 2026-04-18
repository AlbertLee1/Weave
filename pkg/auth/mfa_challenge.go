package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"
)

// DefaultMFAChallengeTTL is how long a challenge token issued by the login
// handler stays valid before the user must re-authenticate. 5 minutes is
// long enough to read a code off an authenticator app on a slow network and
// short enough to keep the window for replay tiny.
const DefaultMFAChallengeTTL = 5 * time.Minute

// ErrMFAChallengeNotFound is returned when a challenge token is unknown
// (never minted, already consumed, or evicted because it expired).
var ErrMFAChallengeNotFound = errors.New("mfa challenge not found")

// ErrMFAChallengeExpired is returned when the token is known but past its
// TTL. Surfaces as 401 InvalidMFAChallenge at the HTTP boundary.
var ErrMFAChallengeExpired = errors.New("mfa challenge expired")

// MFAChallengeStore is the small in-memory cache that bridges the login
// handler (which mints a challenge after successful password verification
// when the user has MFA enabled) and the MFA verify handler (which consumes
// the challenge once the user provides a valid TOTP code). Single-use:
// Consume removes the row regardless of subsequent code-validation outcome
// so a stolen token can't be reused.
type MFAChallengeStore struct {
	mu      sync.Mutex
	entries map[string]mfaChallenge
	ttl     time.Duration
	now     func() time.Time
}

type mfaChallenge struct {
	userID    string
	expiresAt time.Time
}

// NewMFAChallengeStore constructs a store. ttl<=0 falls back to
// DefaultMFAChallengeTTL.
func NewMFAChallengeStore(ttl time.Duration) *MFAChallengeStore {
	if ttl <= 0 {
		ttl = DefaultMFAChallengeTTL
	}
	return &MFAChallengeStore{
		entries: map[string]mfaChallenge{},
		ttl:     ttl,
		now:     time.Now,
	}
}

// SetNowFunc overrides the clock for deterministic TTL tests. Convention
// matches oms.CachedRepository.nowFunc / pkg/oss/computed.Resolver.
func (s *MFAChallengeStore) SetNowFunc(fn func() time.Time) {
	if fn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = fn
}

// Issue mints a fresh opaque challenge token bound to the supplied user ID.
// Returns the token plaintext; callers send it to the client and the client
// must echo it back at /api/auth/mfa/verify.
func (s *MFAChallengeStore) Issue(userID string) (string, error) {
	if userID == "" {
		return "", errors.New("userID required")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)

	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[token] = mfaChallenge{
		userID:    userID,
		expiresAt: s.now().Add(s.ttl),
	}
	s.gc()
	return token, nil
}

// Consume removes the challenge from the store and returns its bound user
// ID. The row is removed even if the caller subsequently rejects the code
// (single-use semantics) — the SPA must request a fresh challenge by going
// through /api/auth/login again.
func (s *MFAChallengeStore) Consume(token string) (string, error) {
	if token == "" {
		return "", ErrMFAChallengeNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[token]
	if !ok {
		return "", ErrMFAChallengeNotFound
	}
	delete(s.entries, token)
	if !s.now().Before(entry.expiresAt) {
		return "", ErrMFAChallengeExpired
	}
	return entry.userID, nil
}

// gc evicts expired entries. Callers must hold s.mu.
func (s *MFAChallengeStore) gc() {
	now := s.now()
	for tok, entry := range s.entries {
		if !now.Before(entry.expiresAt) {
			delete(s.entries, tok)
		}
	}
}

// Size returns the number of currently-stored entries (post-gc). Exposed
// for tests asserting on eviction behaviour.
func (s *MFAChallengeStore) Size() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gc()
	return len(s.entries)
}
