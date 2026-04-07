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

// APIKeyRepository is the persistence interface for the api_keys table.
//
// GetByPrefix only returns active (non-revoked) rows. Expiry is intentionally
// NOT filtered at the repo layer so the middleware can distinguish "expired"
// from "missing" and return a precise error.
type APIKeyRepository interface {
	Create(ctx context.Context, k *APIKeyRecord) error
	GetByPrefix(ctx context.Context, prefix string) (*APIKeyRecord, error)
	GetByID(ctx context.Context, id string) (*APIKeyRecord, error)
	ListByUser(ctx context.Context, userID string) ([]*APIKeyRecord, error)
	Revoke(ctx context.Context, id string) error
	TouchLastUsed(ctx context.Context, id string, when time.Time) error
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

const apiKeyColumns = `id, key_hash, key_prefix, user_id, name, scopes, created_at, expires_at, revoked_at, last_used_at`

func scanAPIKey(row pgx.Row) (*APIKeyRecord, error) {
	rec := &APIKeyRecord{}
	var scopes []string
	var expiresAt, revokedAt, lastUsedAt *time.Time
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
