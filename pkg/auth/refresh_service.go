package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Sentinel errors for refresh-token rotation logic. Tests and handlers match
// against these via direct equality (they are package-level vars, not wrapped).
var (
	ErrRefreshTokenExpired       = errors.New("refresh token expired")
	ErrRefreshTokenRevoked       = errors.New("refresh token revoked")
	ErrRefreshTokenReuseDetected = errors.New("refresh token reuse detected")
)

// RefreshServiceOptions configures token lifetime.
type RefreshServiceOptions struct {
	AbsoluteTTL time.Duration
}

// RefreshService implements opaque rotating refresh tokens. The plaintext
// token is a 32-byte random base64url string; only its SHA-256 hash is
// stored. Every successful Rotate revokes the old row, inserts a new row,
// and chains them by parent_id. Re-using a revoked token kills the entire
// rotation chain (RFC 9700 best practice).
type RefreshService struct {
	store RefreshStore
	ttl   time.Duration
}

// NewRefreshService constructs a service. ttl<=0 falls back to 7 days.
func NewRefreshService(store RefreshStore, opts RefreshServiceOptions) *RefreshService {
	ttl := opts.AbsoluteTTL
	if ttl == 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &RefreshService{store: store, ttl: ttl}
}

// HashRefreshToken returns the SHA-256 hex digest of plaintext. Exported so
// callers (handlers, integration tests) can hash before lookup.
func HashRefreshToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// generatePlaintext returns a 32-byte (256-bit) random token in base64url.
func generatePlaintext() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Generate creates a new refresh token (without a parent), persists it, and
// returns the plaintext (to send to the client) plus the stored record.
// parentID may be "" for fresh logins.
func (s *RefreshService) Generate(ctx context.Context, userID, parentID string) (string, *RefreshTokenRecord, error) {
	plain, err := generatePlaintext()
	if err != nil {
		return "", nil, err
	}
	rec := &RefreshTokenRecord{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: HashRefreshToken(plain),
		IssuedAt:  time.Now(),
		ExpiresAt: time.Now().Add(s.ttl),
		ParentID:  parentID,
	}
	if err := s.store.Create(ctx, rec); err != nil {
		return "", nil, err
	}
	return plain, rec, nil
}

// Lookup returns the persisted record for the given plaintext, regardless of
// revocation/expiry state. Callers (Rotate) decide what to do with revoked
// or expired records.
func (s *RefreshService) Lookup(ctx context.Context, plain string) (*RefreshTokenRecord, error) {
	return s.store.GetByHash(ctx, HashRefreshToken(plain))
}

// Rotate performs the single-use rotation algorithm described in
// FINDING:JWT_6 of the design doc:
//
//  1. Lookup the presented token by hash.
//  2. Not found        → ErrRefreshTokenNotFound
//  3. Revoked already  → ErrRefreshTokenReuseDetected + revoke entire chain
//  4. Expired          → ErrRefreshTokenExpired
//  5. Otherwise        → revoke old, insert new with parent_id chained.
func (s *RefreshService) Rotate(ctx context.Context, plain string) (string, *RefreshTokenRecord, error) {
	old, err := s.store.GetByHash(ctx, HashRefreshToken(plain))
	if err != nil {
		return "", nil, err
	}
	if old.IsRevoked() {
		// Reuse detection: someone is presenting a token we already rotated
		// past. Burn the whole user's chain.
		_ = s.store.RevokeChainForUser(ctx, old.UserID, "reuse_detected")
		return "", nil, ErrRefreshTokenReuseDetected
	}
	if old.IsExpired(time.Now()) {
		return "", nil, ErrRefreshTokenExpired
	}

	// Mark old revoked, insert new chained child.
	if err := s.store.Revoke(ctx, old.ID, "rotated"); err != nil {
		return "", nil, err
	}
	return s.Generate(ctx, old.UserID, old.ID)
}

// Revoke explicitly revokes a token by its plaintext (used by logout).
// Best-effort: missing/already-revoked tokens return nil so logout is
// idempotent.
func (s *RefreshService) Revoke(ctx context.Context, plain string) error {
	rec, err := s.store.GetByHash(ctx, HashRefreshToken(plain))
	if err != nil {
		if errors.Is(err, ErrRefreshTokenNotFound) {
			return nil
		}
		return err
	}
	if rec.IsRevoked() {
		return nil
	}
	return s.store.Revoke(ctx, rec.ID, "logout")
}
