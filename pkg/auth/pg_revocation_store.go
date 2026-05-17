package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGRevocationStore is the Postgres-backed RevocationStore backing the
// US-491 JWT revocation blacklist. It maps onto the auth_revoked_tokens
// table from migration 000211.
type PGRevocationStore struct {
	pool *pgxpool.Pool
}

// NewPGRevocationStore wraps a pgx pool as a RevocationStore.
func NewPGRevocationStore(pool *pgxpool.Pool) *PGRevocationStore {
	return &PGRevocationStore{pool: pool}
}

// Revoke inserts a row into auth_revoked_tokens. ON CONFLICT lets repeated
// revoke calls update the metadata (e.g. reason / user_id) without violating
// the primary-key constraint — admin retries should be idempotent.
func (s *PGRevocationStore) Revoke(ctx context.Context, rec RevocationRecord) error {
	if rec.JTI == "" {
		return ErrRevocationInvalid
	}
	if rec.RevokedAt.IsZero() {
		rec.RevokedAt = time.Now().UTC()
	}
	if rec.ExpiresAt.IsZero() {
		// Without an exp from the original token, fall back to a far-future
		// timestamp so the sweep never prematurely prunes the row.
		rec.ExpiresAt = rec.RevokedAt.Add(24 * time.Hour)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO auth_revoked_tokens (jti, user_id, expires_at, revoked_at, reason)
		 VALUES ($1, NULLIF($2, ''), $3, $4, NULLIF($5, ''))
		 ON CONFLICT (jti) DO UPDATE
		   SET user_id = EXCLUDED.user_id,
		       expires_at = EXCLUDED.expires_at,
		       revoked_at = EXCLUDED.revoked_at,
		       reason = EXCLUDED.reason`,
		rec.JTI, rec.UserID, rec.ExpiresAt, rec.RevokedAt, rec.Reason)
	return err
}

// IsRevoked returns true iff jti has a row in auth_revoked_tokens that has
// not yet aged past its original exp claim. Expired rows are treated as
// not-revoked because the JWT itself would already fail the verifier's
// `exp` check upstream.
func (s *PGRevocationStore) IsRevoked(ctx context.Context, jti string) (bool, error) {
	if jti == "" {
		return false, nil
	}
	var present bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM auth_revoked_tokens
		               WHERE jti = $1 AND expires_at > now())`, jti).Scan(&present)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return present, nil
}

// ReapExpired deletes rows whose `exp` is at or before `before`. Returns the
// number of rows removed so the boot loop can log non-zero sweeps.
func (s *PGRevocationStore) ReapExpired(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM auth_revoked_tokens WHERE expires_at <= $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RunRevocationReaperLoop drives the periodic prune of naturally-expired
// blacklist rows. Mirrors RunActionJobReaperLoop / RunSavedSetReaperLoop:
// free-function so unit tests can inject a fake reaper without spinning a
// real PG container.
func RunRevocationReaperLoop(ctx context.Context, store RevocationStore, interval time.Duration,
	onReap func(int64), onError func(error)) {
	if store == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := store.ReapExpired(ctx, time.Now())
			if err != nil {
				if onError != nil {
					onError(err)
				}
				continue
			}
			if n > 0 && onReap != nil {
				onReap(n)
			}
		}
	}
}
