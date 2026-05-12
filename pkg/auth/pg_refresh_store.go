package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGRefreshStore is a Postgres-backed RefreshStore. It maps directly to the
// refresh_tokens table from migration 000008.
type PGRefreshStore struct {
	pool *pgxpool.Pool
}

// NewPGRefreshStore wraps a pgx pool as a RefreshStore.
func NewPGRefreshStore(pool *pgxpool.Pool) *PGRefreshStore {
	return &PGRefreshStore{pool: pool}
}

func (s *PGRefreshStore) Create(ctx context.Context, t *RefreshTokenRecord) error {
	if t.ID == "" {
		return errors.New("refresh token: id required")
	}
	var parent any
	if t.ParentID != "" {
		parent = t.ParentID
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, issued_at, expires_at, parent_id, user_agent, ip)
		 VALUES ($1, $2, $3, COALESCE($4, now()), $5, $6, NULLIF($7, ''), NULLIF($8, ''))`,
		t.ID, t.UserID, t.TokenHash, nullableTime(t.IssuedAt), t.ExpiresAt, parent, t.UserAgent, t.IP)
	return err
}

func (s *PGRefreshStore) GetByHash(ctx context.Context, hash string) (*RefreshTokenRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, issued_at, expires_at, last_used_at, revoked_at,
		        COALESCE(revocation_reason, ''), COALESCE(user_agent, ''), COALESCE(ip, ''),
		        COALESCE(parent_id::text, '')
		 FROM refresh_tokens WHERE token_hash = $1`, hash)
	rec := &RefreshTokenRecord{}
	var lastUsed, revokedAt *time.Time
	err := row.Scan(&rec.ID, &rec.UserID, &rec.TokenHash, &rec.IssuedAt, &rec.ExpiresAt,
		&lastUsed, &revokedAt, &rec.RevocationReason, &rec.UserAgent, &rec.IP, &rec.ParentID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRefreshTokenNotFound
		}
		return nil, err
	}
	rec.LastUsedAt = lastUsed
	rec.RevokedAt = revokedAt
	return rec, nil
}

func (s *PGRefreshStore) Revoke(ctx context.Context, id, reason string) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now(), revocation_reason = $2 WHERE id = $1 AND revoked_at IS NULL`,
		id, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either not found or already revoked. Treat the latter as success
		// to keep the rotation algorithm idempotent.
		return nil
	}
	return nil
}

// RevokeIfActive performs the SQL-level CAS via a partial-update WHERE clause:
// only rows whose revoked_at is still NULL are touched. The boolean return
// reports whether THIS statement was the one that flipped the bit, letting
// the Rotate path detect a concurrent rotation that already burned the token.
func (s *PGRefreshStore) RevokeIfActive(ctx context.Context, id, reason string) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now(), revocation_reason = $2 WHERE id = $1 AND revoked_at IS NULL`,
		id, reason)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (s *PGRefreshStore) RevokeChainForUser(ctx context.Context, userID, reason string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now(), revocation_reason = $2
		 WHERE user_id = $1 AND revoked_at IS NULL`,
		userID, reason)
	return err
}

func (s *PGRefreshStore) RevokeAllForUser(ctx context.Context, userID, reason string) error {
	return s.RevokeChainForUser(ctx, userID, reason)
}

func (s *PGRefreshStore) MarkUsed(ctx context.Context, id string, when time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET last_used_at = $2 WHERE id = $1`, id, when)
	return err
}

func nullableTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
