package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGSessionStore is a Postgres-backed SessionStore against the table created
// by migration 000054.
type PGSessionStore struct {
	pool *pgxpool.Pool
}

// NewPGSessionStore wraps a pgx pool as a SessionStore.
func NewPGSessionStore(pool *pgxpool.Pool) *PGSessionStore {
	return &PGSessionStore{pool: pool}
}

// Create inserts a session row. If ID is empty the DB-side gen_random_uuid()
// DEFAULT is used and the generated ID is written back into the passed
// record.
func (s *PGSessionStore) Create(ctx context.Context, r *SessionRecord) error {
	if r == nil || r.UserID == "" {
		return errors.New("session: user_id required")
	}
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	var rtID any
	if r.RefreshTokenID != "" {
		rtID = r.RefreshTokenID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO sessions (id, user_id, refresh_token_id, ip, user_agent, created_at, last_seen)
		 VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), COALESCE($6, now()), COALESCE($7, now()))`,
		r.ID, r.UserID, rtID, r.IP, r.UserAgent, nullableTime(r.CreatedAt), nullableTime(r.LastSeen))
	return err
}

// Get returns a session by id. Returns ErrSessionNotFound when absent.
func (s *PGSessionStore) Get(ctx context.Context, id string) (*SessionRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, COALESCE(refresh_token_id::text, ''), COALESCE(ip, ''), COALESCE(user_agent, ''), created_at, last_seen
		 FROM sessions WHERE id = $1`, id)
	rec := &SessionRecord{}
	if err := row.Scan(&rec.ID, &rec.UserID, &rec.RefreshTokenID, &rec.IP, &rec.UserAgent, &rec.CreatedAt, &rec.LastSeen); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return rec, nil
}

// ListByUser returns every session bound to the supplied user, ordered by
// last_seen descending.
func (s *PGSessionStore) ListByUser(ctx context.Context, userID string) ([]*SessionRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, COALESCE(refresh_token_id::text, ''), COALESCE(ip, ''), COALESCE(user_agent, ''), created_at, last_seen
		 FROM sessions WHERE user_id = $1 ORDER BY last_seen DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SessionRecord
	for rows.Next() {
		rec := &SessionRecord{}
		if err := rows.Scan(&rec.ID, &rec.UserID, &rec.RefreshTokenID, &rec.IP, &rec.UserAgent, &rec.CreatedAt, &rec.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Delete removes a session iff callerUserID owns it.
func (s *PGSessionStore) Delete(ctx context.Context, id, callerUserID string) error {
	// Distinguish not-found from forbidden so the handler can emit 404 vs
	// 403. Two-step: look up the owner, then delete.
	var owner string
	err := s.pool.QueryRow(ctx, `SELECT user_id FROM sessions WHERE id = $1`, id).Scan(&owner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSessionNotFound
		}
		return err
	}
	if owner != callerUserID {
		return ErrSessionForbidden
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

// DeleteAllForUser removes every session row for the supplied user.
// Idempotent.
func (s *PGSessionStore) DeleteAllForUser(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

// Touch updates last_seen on an existing session.
func (s *PGSessionStore) Touch(ctx context.Context, id string, when time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE sessions SET last_seen = COALESCE($2, now()) WHERE id = $1`,
		id, nullableTime(when))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

// RotateRefreshToken rebinds sessions bound to oldRefreshID onto newRefreshID
// and bumps last_seen. Missing rows is a no-op — the login flow may have run
// before the session store was wired.
func (s *PGSessionStore) RotateRefreshToken(ctx context.Context, oldRefreshID, newRefreshID string, when time.Time) error {
	if oldRefreshID == "" {
		return nil
	}
	var newID any
	if newRefreshID != "" {
		newID = newRefreshID
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE sessions
		    SET refresh_token_id = $2,
		        last_seen        = COALESCE($3, now())
		  WHERE refresh_token_id = $1`,
		oldRefreshID, newID, nullableTime(when))
	return err
}
