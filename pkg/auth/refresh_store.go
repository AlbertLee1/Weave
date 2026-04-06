package auth

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrRefreshTokenNotFound is returned when a hash lookup misses.
var ErrRefreshTokenNotFound = errors.New("refresh token not found")

// RefreshTokenRecord is the persisted shape of a refresh token. It mirrors
// the refresh_tokens table in migration 000008.
type RefreshTokenRecord struct {
	ID               string
	UserID           string
	TokenHash        string
	IssuedAt         time.Time
	ExpiresAt        time.Time
	LastUsedAt       *time.Time
	RevokedAt        *time.Time
	RevocationReason string
	UserAgent        string
	IP               string
	ParentID         string
}

// IsRevoked reports whether the token has been marked revoked.
func (r *RefreshTokenRecord) IsRevoked() bool { return r.RevokedAt != nil }

// IsExpired reports whether the token's absolute TTL has elapsed.
func (r *RefreshTokenRecord) IsExpired(now time.Time) bool { return now.After(r.ExpiresAt) }

// RefreshStore is the persistence interface for refresh tokens.
type RefreshStore interface {
	Create(ctx context.Context, t *RefreshTokenRecord) error
	GetByHash(ctx context.Context, hash string) (*RefreshTokenRecord, error)
	Revoke(ctx context.Context, id, reason string) error
	RevokeChainForUser(ctx context.Context, userID, reason string) error
	RevokeAllForUser(ctx context.Context, userID, reason string) error
	MarkUsed(ctx context.Context, id string, when time.Time) error
}

// MemoryRefreshStore is an in-memory RefreshStore used by unit tests and
// dev mode. It is safe for concurrent use.
type MemoryRefreshStore struct {
	mu     sync.RWMutex
	byID   map[string]*RefreshTokenRecord
	byHash map[string]string // hash -> id
}

// NewMemoryRefreshStore returns an empty in-memory store.
func NewMemoryRefreshStore() *MemoryRefreshStore {
	return &MemoryRefreshStore{
		byID:   map[string]*RefreshTokenRecord{},
		byHash: map[string]string{},
	}
}

func (s *MemoryRefreshStore) Create(_ context.Context, t *RefreshTokenRecord) error {
	if t == nil || t.ID == "" || t.TokenHash == "" {
		return errors.New("refresh token: id and hash are required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.IssuedAt.IsZero() {
		t.IssuedAt = time.Now()
	}
	cp := *t
	s.byID[t.ID] = &cp
	s.byHash[t.TokenHash] = t.ID
	return nil
}

func (s *MemoryRefreshStore) GetByHash(_ context.Context, hash string) (*RefreshTokenRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	id, ok := s.byHash[hash]
	if !ok {
		return nil, ErrRefreshTokenNotFound
	}
	rec := s.byID[id]
	if rec == nil {
		return nil, ErrRefreshTokenNotFound
	}
	cp := *rec
	return &cp, nil
}

func (s *MemoryRefreshStore) Revoke(_ context.Context, id, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return ErrRefreshTokenNotFound
	}
	now := time.Now()
	rec.RevokedAt = &now
	rec.RevocationReason = reason
	return nil
}

func (s *MemoryRefreshStore) RevokeChainForUser(_ context.Context, userID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, rec := range s.byID {
		if rec.UserID != userID {
			continue
		}
		if rec.RevokedAt != nil {
			continue
		}
		t := now
		rec.RevokedAt = &t
		rec.RevocationReason = reason
	}
	return nil
}

func (s *MemoryRefreshStore) RevokeAllForUser(ctx context.Context, userID, reason string) error {
	return s.RevokeChainForUser(ctx, userID, reason)
}

func (s *MemoryRefreshStore) MarkUsed(_ context.Context, id string, when time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return ErrRefreshTokenNotFound
	}
	t := when
	rec.LastUsedAt = &t
	return nil
}
