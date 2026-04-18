package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrAPIKeyNotFound is returned when a prefix lookup misses (or matches a row
// that has already been revoked, since the unique partial index excludes
// revoked rows from the active key set).
var ErrAPIKeyNotFound = errors.New("api key not found")

// ErrAPIKeyAlreadyRotated is returned by Rotate when the predecessor row
// already has a SuccessorID set. Rotation is one-shot: a fresh rotation
// against an already-rotated key would orphan the prior successor.
var ErrAPIKeyAlreadyRotated = errors.New("api key already rotated")

// APIKeyRepository is the persistence interface for the api_keys table.
//
// GetByPrefix only returns active (non-revoked) rows. Expiry is intentionally
// NOT filtered at the repo layer so the middleware can distinguish "expired"
// from "missing" and return a precise error.
//
// Rotation flow: Rotate inserts the successor row (via the same columns as
// Create) and stamps the predecessor's rotates_at + successor_id in a single
// transaction. ListPendingRotations surfaces non-revoked keys whose
// scheduled rotation falls inside the supplied look-ahead window so
// operators can warn ahead of the cut-off.
type APIKeyRepository interface {
	Create(ctx context.Context, k *APIKeyRecord) error
	GetByPrefix(ctx context.Context, prefix string) (*APIKeyRecord, error)
	GetByID(ctx context.Context, id string) (*APIKeyRecord, error)
	ListByUser(ctx context.Context, userID string) ([]*APIKeyRecord, error)
	Revoke(ctx context.Context, id string) error
	TouchLastUsed(ctx context.Context, id string, when time.Time) error
	Rotate(ctx context.Context, predecessorID string, successor *APIKeyRecord, graceUntil time.Time) error
	ListPendingRotations(ctx context.Context, now time.Time, within time.Duration) ([]*APIKeyRecord, error)
}

// PGAPIKeyRepository is the Postgres-backed APIKeyRepository.
type PGAPIKeyRepository struct {
	pool *pgxpool.Pool
}

// NewPGAPIKeyRepository wraps a pgx pool as an APIKeyRepository.
func NewPGAPIKeyRepository(pool *pgxpool.Pool) *PGAPIKeyRepository {
	return &PGAPIKeyRepository{pool: pool}
}

// Create inserts a new key row. The caller is responsible for populating
// KeyHash and KeyPrefix; ID and CreatedAt are populated from the DB defaults
// and copied back onto the supplied record.
func (r *PGAPIKeyRepository) Create(ctx context.Context, k *APIKeyRecord) error {
	if k == nil {
		return errors.New("api key: nil record")
	}
	if len(k.KeyHash) == 0 || k.KeyPrefix == "" || k.UserID == "" || k.Name == "" {
		return errors.New("api key: hash, prefix, user_id and name are required")
	}
	scopes := k.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO api_keys (key_hash, key_prefix, user_id, name, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		k.KeyHash, k.KeyPrefix, k.UserID, k.Name, scopes, k.ExpiresAt)
	return row.Scan(&k.ID, &k.CreatedAt)
}

const apiKeyColumns = `id, key_hash, key_prefix, user_id, name, scopes, created_at, expires_at, revoked_at, last_used_at, rotates_at, successor_id`

func scanAPIKey(row pgx.Row) (*APIKeyRecord, error) {
	rec := &APIKeyRecord{}
	var scopes []string
	var expiresAt, revokedAt, lastUsedAt, rotatesAt *time.Time
	var successorID *string
	err := row.Scan(
		&rec.ID,
		&rec.KeyHash,
		&rec.KeyPrefix,
		&rec.UserID,
		&rec.Name,
		&scopes,
		&rec.CreatedAt,
		&expiresAt,
		&revokedAt,
		&lastUsedAt,
		&rotatesAt,
		&successorID,
	)
	if err != nil {
		return nil, err
	}
	rec.Scopes = scopes
	if rec.Scopes == nil {
		rec.Scopes = []string{}
	}
	rec.ExpiresAt = expiresAt
	rec.RevokedAt = revokedAt
	rec.LastUsedAt = lastUsedAt
	rec.RotatesAt = rotatesAt
	rec.SuccessorID = successorID
	return rec, nil
}

// GetByPrefix returns the active key matching the prefix, or
// ErrAPIKeyNotFound. Revoked rows are excluded by the WHERE clause; the
// caller MUST still constant-time compare KeyHash before trusting the row.
func (r *PGAPIKeyRepository) GetByPrefix(ctx context.Context, prefix string) (*APIKeyRecord, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+apiKeyColumns+`
		 FROM api_keys
		 WHERE key_prefix = $1 AND revoked_at IS NULL`, prefix)
	rec, err := scanAPIKey(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, err
	}
	return rec, nil
}

// GetByID returns the row by primary key regardless of revocation state.
// Used by Revoke (and admin handlers) to verify ownership before mutating.
func (r *PGAPIKeyRepository) GetByID(ctx context.Context, id string) (*APIKeyRecord, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+apiKeyColumns+`
		 FROM api_keys
		 WHERE id = $1`, id)
	rec, err := scanAPIKey(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAPIKeyNotFound
		}
		return nil, err
	}
	return rec, nil
}

// ListByUser returns the user's active (non-revoked) keys, newest first.
func (r *PGAPIKeyRepository) ListByUser(ctx context.Context, userID string) ([]*APIKeyRecord, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+apiKeyColumns+`
		 FROM api_keys
		 WHERE user_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKeyRecord
	for rows.Next() {
		rec, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Revoke marks the key revoked. Idempotent: rerunning against an already
// revoked row returns nil so admin double-clicks do not produce errors.
func (r *PGAPIKeyRepository) Revoke(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = now()
		 WHERE id = $1 AND revoked_at IS NULL`, id)
	return err
}

// TouchLastUsed updates last_used_at for the row. Best-effort: callers
// (typically the auth middleware) should fire-and-forget this so the request
// path is not blocked on the write.
func (r *PGAPIKeyRepository) TouchLastUsed(ctx context.Context, id string, when time.Time) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = $2 WHERE id = $1`, id, when)
	return err
}

// Rotate inserts the successor row AND stamps the predecessor's rotates_at +
// successor_id atomically. The caller owns the successor's hash / prefix /
// user / name / scopes (typically mirroring the predecessor's non-secret
// metadata); Create-style rules apply.
//
// Errors:
//   - ErrAPIKeyNotFound: predecessor id does not exist or has been revoked.
//   - ErrAPIKeyAlreadyRotated: predecessor already carries a successor_id.
//
// The predecessor's expires_at, revoked_at, last_used_at are never touched —
// once graceUntil lands, the middleware rejects the key via
// IsRotationExpired, but the row itself is preserved as an audit trail.
func (r *PGAPIKeyRepository) Rotate(ctx context.Context, predecessorID string, successor *APIKeyRecord, graceUntil time.Time) error {
	if successor == nil {
		return errors.New("api key: nil successor record")
	}
	if len(successor.KeyHash) == 0 || successor.KeyPrefix == "" || successor.UserID == "" || successor.Name == "" {
		return errors.New("api key: successor hash, prefix, user_id and name are required")
	}
	scopes := successor.Scopes
	if scopes == nil {
		scopes = []string{}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var existingSuccessor *string
	var revokedAt *time.Time
	err = tx.QueryRow(ctx,
		`SELECT successor_id, revoked_at FROM api_keys WHERE id = $1 FOR UPDATE`,
		predecessorID).Scan(&existingSuccessor, &revokedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAPIKeyNotFound
		}
		return err
	}
	if revokedAt != nil {
		return ErrAPIKeyNotFound
	}
	if existingSuccessor != nil {
		return ErrAPIKeyAlreadyRotated
	}

	row := tx.QueryRow(ctx,
		`INSERT INTO api_keys (key_hash, key_prefix, user_id, name, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, created_at`,
		successor.KeyHash, successor.KeyPrefix, successor.UserID, successor.Name, scopes, successor.ExpiresAt)
	if err := row.Scan(&successor.ID, &successor.CreatedAt); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE api_keys SET rotates_at = $2, successor_id = $3 WHERE id = $1`,
		predecessorID, graceUntil, successor.ID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ListPendingRotations returns non-revoked keys whose scheduled rotation
// falls inside [now, now+within]. Callers (e.g. a warning-emission cron)
// iterate the result to notify owning services. Ordered by rotates_at
// ascending so the soonest cut-off surfaces first.
func (r *PGAPIKeyRepository) ListPendingRotations(ctx context.Context, now time.Time, within time.Duration) ([]*APIKeyRecord, error) {
	if within < 0 {
		within = 0
	}
	cutoff := now.Add(within)
	rows, err := r.pool.Query(ctx,
		`SELECT `+apiKeyColumns+`
		 FROM api_keys
		 WHERE revoked_at IS NULL
		   AND rotates_at IS NOT NULL
		   AND rotates_at >= $1
		   AND rotates_at <= $2
		 ORDER BY rotates_at ASC`, now, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIKeyRecord
	for rows.Next() {
		rec, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
