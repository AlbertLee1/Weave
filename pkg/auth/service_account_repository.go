package auth

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrServiceAccountNotFound is returned by Get / Update / Disable when the
// id does not address a row (or when the row has already been disabled and
// the caller asked for active-only).
var ErrServiceAccountNotFound = errors.New("service account not found")

// ErrServiceAccountNameConflict is returned by Create / UpdateServiceAccount
// when another active service account already holds the requested name.
var ErrServiceAccountNameConflict = errors.New("service account name already in use")

// ServiceAccountRepository is the narrow persistence surface for US-249.
// The admin handlers talk to this interface; the PG implementation below is
// the production binding, and tests substitute an in-memory fake.
//
// Operations:
//   - Create      inserts a new row and populates ID / CreatedAt / UpdatedAt
//   - GetByID     returns the row by primary key regardless of disabled state
//   - GetByName   returns the ACTIVE (not-disabled) row by name, or
//                 ErrServiceAccountNotFound
//   - ListActive  returns all non-disabled rows, newest first
//   - Update      applies a partial PATCH (see ServiceAccountUpdate)
//   - Disable     soft-deletes by stamping disabled_at=now()
type ServiceAccountRepository interface {
	Create(ctx context.Context, sa *ServiceAccount) error
	GetByID(ctx context.Context, id string) (*ServiceAccount, error)
	GetByName(ctx context.Context, name string) (*ServiceAccount, error)
	ListActive(ctx context.Context) ([]*ServiceAccount, error)
	Update(ctx context.Context, id string, upd ServiceAccountUpdate) (*ServiceAccount, error)
	Disable(ctx context.Context, id string) error
}

// ServiceAccountUpdate is the partial-PATCH shape accepted by Update.
//
// All fields are pointers so callers distinguish omit (nil) from explicit
// clear (non-nil with zero value). Mirrors the convention used by other
// Update DTOs in the codebase (UpdateLinkTypeRequest.IsRequired *bool,
// UpdateActionTypeRequest.ParameterSchema *json.RawMessage, etc.).
//
//   - Description: nil = preserve, non-nil = replace (empty string clears)
//   - Scopes:      nil = preserve, non-nil = replace with the supplied slice
//   - ExpiresAt:   nil pointer = preserve; non-nil pointer-to-nil-time =
//                  clear absolute expiry; pointer-to-time = set to that
//                  instant.
type ServiceAccountUpdate struct {
	Description *string
	Scopes      *[]string
	ExpiresAt   **time.Time
}

// PGServiceAccountRepository is the Postgres-backed ServiceAccountRepository.
type PGServiceAccountRepository struct {
	pool *pgxpool.Pool
}

// NewPGServiceAccountRepository wraps a pgx pool as a
// ServiceAccountRepository.
func NewPGServiceAccountRepository(pool *pgxpool.Pool) *PGServiceAccountRepository {
	return &PGServiceAccountRepository{pool: pool}
}

const serviceAccountColumns = `id, name, description, owner_user_id, scopes, expires_at, disabled_at, created_at, updated_at`

// Create persists a new service account. Caller must populate Name +
// OwnerUserID; everything else is optional. ID / CreatedAt / UpdatedAt are
// stamped by the DB and copied back onto the supplied record.
func (r *PGServiceAccountRepository) Create(ctx context.Context, sa *ServiceAccount) error {
	if sa == nil {
		return errors.New("service account: nil record")
	}
	if sa.Name == "" || sa.OwnerUserID == "" {
		return errors.New("service account: name and owner_user_id are required")
	}
	scopes := sa.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	description := sa.Description
	row := r.pool.QueryRow(ctx,
		`INSERT INTO service_accounts (name, description, owner_user_id, scopes, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, created_at, updated_at`,
		sa.Name, description, sa.OwnerUserID, scopes, sa.ExpiresAt)
	if err := row.Scan(&sa.ID, &sa.CreatedAt, &sa.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return ErrServiceAccountNameConflict
		}
		return err
	}
	sa.Scopes = scopes
	sa.Description = description
	return nil
}

// isUniqueViolation reports whether the error is a PostgreSQL unique-index
// violation (SQLSTATE 23505). Used to translate the name-uniqueness index
// collision into ErrServiceAccountNameConflict without string-matching the
// underlying error message.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx surfaces these as *pgconn.PgError; rather than import pgconn
	// directly (which lives in a separate module path) we match on the
	// canonical SQLSTATE marker that appears in the error string. Every
	// driver produces the same marker.
	return strings.Contains(err.Error(), "SQLSTATE 23505") ||
		strings.Contains(err.Error(), "duplicate key value")
}

func scanServiceAccount(row pgx.Row) (*ServiceAccount, error) {
	sa := &ServiceAccount{}
	var scopes []string
	var expiresAt, disabledAt *time.Time
	err := row.Scan(
		&sa.ID,
		&sa.Name,
		&sa.Description,
		&sa.OwnerUserID,
		&scopes,
		&expiresAt,
		&disabledAt,
		&sa.CreatedAt,
		&sa.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	sa.Scopes = scopes
	if sa.Scopes == nil {
		sa.Scopes = []string{}
	}
	sa.ExpiresAt = expiresAt
	sa.DisabledAt = disabledAt
	return sa, nil
}

// GetByID returns the row regardless of disabled state; callers that want
// active-only must check sa.IsDisabled().
func (r *PGServiceAccountRepository) GetByID(ctx context.Context, id string) (*ServiceAccount, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+serviceAccountColumns+` FROM service_accounts WHERE id = $1`, id)
	sa, err := scanServiceAccount(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServiceAccountNotFound
		}
		return nil, err
	}
	return sa, nil
}

// GetByName returns the ACTIVE row with the supplied name, or
// ErrServiceAccountNotFound. Disabled rows are skipped so a recreated
// service account resolves to the new row.
func (r *PGServiceAccountRepository) GetByName(ctx context.Context, name string) (*ServiceAccount, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+serviceAccountColumns+`
		 FROM service_accounts
		 WHERE name = $1 AND disabled_at IS NULL`, name)
	sa, err := scanServiceAccount(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServiceAccountNotFound
		}
		return nil, err
	}
	return sa, nil
}

// ListActive returns every non-disabled service account, newest first.
// v1 does not paginate — the expected cardinality is <<100 rows per org.
func (r *PGServiceAccountRepository) ListActive(ctx context.Context) ([]*ServiceAccount, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+serviceAccountColumns+`
		 FROM service_accounts
		 WHERE disabled_at IS NULL
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ServiceAccount
	for rows.Next() {
		sa, err := scanServiceAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sa)
	}
	return out, rows.Err()
}

// Update applies a partial PATCH. Fields left nil on ServiceAccountUpdate
// are preserved; non-nil fields are written regardless of value. The
// updated_at column is always bumped to now() by the handler so every row
// carries an accurate "last modified" timestamp.
//
// Returns the updated row as read back from the DB so callers don't have
// to second-guess whether the change landed.
func (r *PGServiceAccountRepository) Update(ctx context.Context, id string, upd ServiceAccountUpdate) (*ServiceAccount, error) {
	sets := []string{}
	args := []interface{}{}
	if upd.Description != nil {
		sets = append(sets, "description = $"+strconv.Itoa(len(args)+1))
		args = append(args, *upd.Description)
	}
	if upd.Scopes != nil {
		scopes := *upd.Scopes
		if scopes == nil {
			scopes = []string{}
		}
		sets = append(sets, "scopes = $"+strconv.Itoa(len(args)+1))
		args = append(args, scopes)
	}
	if upd.ExpiresAt != nil {
		sets = append(sets, "expires_at = $"+strconv.Itoa(len(args)+1))
		args = append(args, *upd.ExpiresAt)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	sql := `UPDATE service_accounts SET ` + strings.Join(sets, ", ") +
		` WHERE id = $` + strconv.Itoa(len(args)) + ` AND disabled_at IS NULL`
	ct, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() == 0 {
		return nil, ErrServiceAccountNotFound
	}
	return r.GetByID(ctx, id)
}

// Disable soft-revokes the service account. Idempotent: re-applying against
// an already-disabled row returns nil so admin double-clicks do not error.
func (r *PGServiceAccountRepository) Disable(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE service_accounts
		 SET disabled_at = now(), updated_at = now()
		 WHERE id = $1 AND disabled_at IS NULL`, id)
	return err
}
