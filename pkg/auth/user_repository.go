package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrUserNotFound is returned when a lookup by id or email returns no rows.
var ErrUserNotFound = errors.New("user not found")

// UserRecord is the persistent representation of a user. It is intentionally
// kept separate from auth.User (which is the per-request resolved view) so
// that the storage schema can evolve without churning request handlers.
type UserRecord struct {
	ID           string
	Email        string
	Name         string
	PasswordHash string
	// MFASecret is the user's TOTP shared secret (base32). Empty when the
	// user has never started MFA enrollment. Persistence does not enforce
	// the second factor on its own — see MFAEnabled.
	MFASecret string
	// MFAEnabled is true once the user has confirmed possession of the
	// secret with a valid code via /api/auth/mfa/enable. While true the
	// login handler returns a challenge instead of an access token.
	MFAEnabled bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// UserRepository is the persistence interface for users and role grants.
// It is intentionally kept separate from oms.Repository so existing oms
// mocks do not need to grow when RBAC ships.
type UserRepository interface {
	CreateUser(ctx context.Context, u *UserRecord) error
	GetUserByID(ctx context.Context, id string) (*UserRecord, error)
	GetUserByEmail(ctx context.Context, email string) (*UserRecord, error)
	ListUserRoles(ctx context.Context, userID string) ([]string, error)
	ListUserOntologyRoles(ctx context.Context, userID string) (map[string]string, error)
	UpsertUserRole(ctx context.Context, userID, role string) error
	// SetPassword updates the bcrypt password_hash column for a user.
	// Used by JWT bootstrap and any future password-reset endpoint.
	SetPassword(ctx context.Context, userID, passwordHash string) error
}

// PGUserRepository is the Postgres implementation of UserRepository.
type PGUserRepository struct {
	pool *pgxpool.Pool
}

// NewPGUserRepository constructs a Postgres-backed UserRepository.
func NewPGUserRepository(pool *pgxpool.Pool) *PGUserRepository {
	return &PGUserRepository{pool: pool}
}

func (r *PGUserRepository) CreateUser(ctx context.Context, u *UserRecord) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash, name)
		 VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), NULLIF($4, ''))`,
		u.ID, u.Email, u.PasswordHash, u.Name)
	return err
}

func (r *PGUserRepository) GetUserByID(ctx context.Context, id string) (*UserRecord, error) {
	u := &UserRecord{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, COALESCE(email, ''), COALESCE(password_hash, ''), COALESCE(name, ''),
		        COALESCE(mfa_secret, ''), COALESCE(mfa_enabled, FALSE), created_at, updated_at
		 FROM users WHERE id = $1`, id).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.MFASecret, &u.MFAEnabled, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return u, nil
}

func (r *PGUserRepository) GetUserByEmail(ctx context.Context, email string) (*UserRecord, error) {
	u := &UserRecord{}
	err := r.pool.QueryRow(ctx,
		`SELECT id, COALESCE(email, ''), COALESCE(password_hash, ''), COALESCE(name, ''),
		        COALESCE(mfa_secret, ''), COALESCE(mfa_enabled, FALSE), created_at, updated_at
		 FROM users WHERE email = $1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Name, &u.MFASecret, &u.MFAEnabled, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	return u, nil
}

func (r *PGUserRepository) ListUserRoles(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT role FROM user_roles WHERE user_id = $1 ORDER BY role`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

func (r *PGUserRepository) ListUserOntologyRoles(ctx context.Context, userID string) (map[string]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT ontology_rid, role FROM user_ontology_roles WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var rid, role string
		if err := rows.Scan(&rid, &role); err != nil {
			return nil, err
		}
		out[rid] = role
	}
	return out, rows.Err()
}

func (r *PGUserRepository) UpsertUserRole(ctx context.Context, userID, role string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_roles (user_id, role) VALUES ($1, $2)
		 ON CONFLICT (user_id, role) DO NOTHING`,
		userID, role)
	return err
}

// RevokeUserRole deletes the user's grant of the supplied role. Idempotent:
// removing a role the user does not hold is a no-op.
func (r *PGUserRepository) RevokeUserRole(ctx context.Context, userID, role string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM user_roles WHERE user_id = $1 AND role = $2`,
		userID, role)
	return err
}

// SetPassword writes the bcrypt password_hash for the given user. Returns
// ErrUserNotFound if no user matches.
func (r *PGUserRepository) SetPassword(ctx context.Context, userID, passwordHash string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE users SET password_hash = $1, updated_at = now() WHERE id = $2`,
		passwordHash, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}
