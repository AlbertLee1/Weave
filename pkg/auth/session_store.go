package auth

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrSessionNotFound is returned when a session lookup misses. Surfaces as
// 404 SessionNotFound at the HTTP boundary.
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionForbidden is returned when the caller tries to operate on a
// session they do not own. Surfaces as 403 SessionForbidden.
var ErrSessionForbidden = errors.New("session forbidden")

// SessionRecord is the persisted shape of an active login session. It is
// created when the login / refresh / MFA-verify flow mints a new refresh
// token and is removed (or its refresh row revoked) when the user hits
// DELETE /api/auth/sessions/{id}.
//
// ID is the server-side opaque handle (uuid) — distinct from RefreshTokenID
// so the session row stays stable across refresh rotations. When the session
// is bound to a currently-active refresh token, RefreshTokenID carries the
// refresh_tokens.id handle so /sessions/{id} delete can revoke the rotation
// chain in one round trip.
type SessionRecord struct {
	ID             string
	UserID         string
	RefreshTokenID string
	IP             string
	UserAgent      string
	CreatedAt      time.Time
	LastSeen       time.Time
}

// SessionStore is the narrow persistence surface for session management.
// Kept separate from UserRepository / RefreshStore so the ~15 in-memory
// mocks scattered through the test tree don't need to grow stubs — same
// pattern as MFASecretStore / ServiceAccountRepository (see progress.txt
// "Catalog stores MUST be served by the uncached *PGRepository" and the
// US-249/US-251/US-253 notes).
type SessionStore interface {
	Create(ctx context.Context, s *SessionRecord) error
	Get(ctx context.Context, id string) (*SessionRecord, error)
	ListByUser(ctx context.Context, userID string) ([]*SessionRecord, error)
	Delete(ctx context.Context, id, callerUserID string) error
	DeleteAllForUser(ctx context.Context, userID string) error
	Touch(ctx context.Context, id string, when time.Time) error
	// RotateRefreshToken keeps the session row bound to the current live
	// refresh token across rotations. Callers pass the ID the session was
	// previously bound to and the fresh ID that Rotate just generated; the
	// store updates any row with refresh_token_id=oldID to newID and bumps
	// last_seen. Missing rows (rotation by a caller that didn't create a
	// session at login) is a no-op.
	RotateRefreshToken(ctx context.Context, oldRefreshID, newRefreshID string, when time.Time) error
}

// MemorySessionStore is the in-memory SessionStore. Used by unit tests and
// the degraded-mode router (no PG) so the /api/auth/sessions endpoints still
// behave sensibly when sessions aren't persisted.
type MemorySessionStore struct {
	mu   sync.RWMutex
	rows map[string]*SessionRecord
}

// NewMemorySessionStore returns an empty in-memory store.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{rows: map[string]*SessionRecord{}}
}

// Create inserts a session row. ID and UserID are mandatory.
func (s *MemorySessionStore) Create(_ context.Context, r *SessionRecord) error {
	if r == nil || r.ID == "" {
		return errors.New("session: id required")
	}
	if r.UserID == "" {
		return errors.New("session: user_id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	if r.LastSeen.IsZero() {
		r.LastSeen = r.CreatedAt
	}
	cp := *r
	s.rows[r.ID] = &cp
	return nil
}

// Get returns the session by id. Returns ErrSessionNotFound when absent.
func (s *MemorySessionStore) Get(_ context.Context, id string) (*SessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rows[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	cp := *r
	return &cp, nil
}

// ListByUser returns all sessions for a user sorted by LastSeen descending
// (most recent first) so the SPA can show "this device" at the top.
func (s *MemorySessionStore) ListByUser(_ context.Context, userID string) ([]*SessionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*SessionRecord
	for _, r := range s.rows {
		if r.UserID != userID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out, nil
}

// Delete removes the session only when callerUserID owns it. Returns
// ErrSessionNotFound for unknown IDs and ErrSessionForbidden for owner
// mismatches.
func (s *MemorySessionStore) Delete(_ context.Context, id, callerUserID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return ErrSessionNotFound
	}
	if r.UserID != callerUserID {
		return ErrSessionForbidden
	}
	delete(s.rows, id)
	return nil
}

// DeleteAllForUser removes every session for the supplied user. Idempotent.
func (s *MemorySessionStore) DeleteAllForUser(_ context.Context, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, r := range s.rows {
		if r.UserID == userID {
			delete(s.rows, id)
		}
	}
	return nil
}

// Touch updates the LastSeen timestamp for an existing session. Used by the
// refresh-token rotation path so /sessions lists surface the last activity
// time.
func (s *MemorySessionStore) Touch(_ context.Context, id string, when time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rows[id]
	if !ok {
		return ErrSessionNotFound
	}
	r.LastSeen = when
	return nil
}

// RotateRefreshToken rebinds any session carrying oldRefreshID to newRefreshID
// and bumps its LastSeen. Missing rows is a no-op so callers that didn't
// create a session at login don't error on rotation.
func (s *MemorySessionStore) RotateRefreshToken(_ context.Context, oldRefreshID, newRefreshID string, when time.Time) error {
	if oldRefreshID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rows {
		if r.RefreshTokenID == oldRefreshID {
			r.RefreshTokenID = newRefreshID
			if !when.IsZero() {
				r.LastSeen = when
			}
		}
	}
	return nil
}
