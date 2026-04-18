package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrRoleNotFound is returned when a Role lookup by name returns no rows.
var ErrRoleNotFound = errors.New("role not found")

// ErrRoleConflict is returned when a Create collides with an existing
// role name.
var ErrRoleConflict = errors.New("role name already in use")

// RoleRepository is the narrow persistence surface for the roles +
// role_permissions tables added in migration 000051.
//
//   - Create           registers a new (custom) role
//   - Get              returns a role by name, or ErrRoleNotFound
//   - List             returns all roles, built-ins first then alpha
//   - Delete           removes a role; cascades into role_permissions
//   - UpdateDescription patches the description field (built-ins allowed)
//
// Permission membership:
//
//   - SetPermissions   replaces the role's permission list atomically
//   - ListPermissions  returns the role's permission list
type RoleRepository interface {
	Create(ctx context.Context, role *Role) error
	Get(ctx context.Context, name string) (*Role, error)
	List(ctx context.Context) ([]*Role, error)
	Delete(ctx context.Context, name string) error
	UpdateDescription(ctx context.Context, name, description string) (*Role, error)

	SetPermissions(ctx context.Context, role string, perms []string) error
	ListPermissions(ctx context.Context, role string) ([]string, error)
}

// PGRoleRepository is the Postgres-backed RoleRepository.
type PGRoleRepository struct {
	pool *pgxpool.Pool
}

// NewPGRoleRepository wraps a pgx pool as a RoleRepository.
func NewPGRoleRepository(pool *pgxpool.Pool) *PGRoleRepository {
	return &PGRoleRepository{pool: pool}
}

func (r *PGRoleRepository) Create(ctx context.Context, role *Role) error {
	if role == nil || role.Name == "" {
		return errors.New("role: name is required")
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO roles (name, description, builtin)
		 VALUES ($1, $2, $3)
		 RETURNING created_at`,
		role.Name, role.Description, role.Builtin)
	if err := row.Scan(&role.CreatedAt); err != nil {
		if isUniqueViolation(err) {
			return ErrRoleConflict
		}
		return err
	}
	return nil
}

func scanRole(row pgx.Row) (*Role, error) {
	role := &Role{}
	err := row.Scan(&role.Name, &role.Description, &role.Builtin, &role.CreatedAt)
	if err != nil {
		return nil, err
	}
	return role, nil
}

func (r *PGRoleRepository) Get(ctx context.Context, name string) (*Role, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT name, description, builtin, created_at FROM roles WHERE name = $1`, name)
	role, err := scanRole(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}
	return role, nil
}

// List returns all roles, built-ins first then alphabetical.
func (r *PGRoleRepository) List(ctx context.Context) ([]*Role, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT name, description, builtin, created_at FROM roles
		 ORDER BY builtin DESC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Role
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

// Delete removes a custom role. Permissions cascade via role_permissions FK.
// Callers must pre-check the builtin flag and reject deletes before calling
// this (ErrBuiltinRoleProtected at the handler layer).
func (r *PGRoleRepository) Delete(ctx context.Context, name string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM roles WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}

// UpdateDescription patches only the description column.
func (r *PGRoleRepository) UpdateDescription(ctx context.Context, name, description string) (*Role, error) {
	ct, err := r.pool.Exec(ctx,
		`UPDATE roles SET description = $1 WHERE name = $2`,
		description, name)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() == 0 {
		return nil, ErrRoleNotFound
	}
	return r.Get(ctx, name)
}

// SetPermissions replaces the role's permission list atomically — delete
// old rows, insert new ones, all inside a single transaction so a
// concurrent reader never observes a half-applied list.
func (r *PGRoleRepository) SetPermissions(ctx context.Context, role string, perms []string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE name = $1)`, role).
		Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrRoleNotFound
	}

	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_name = $1`, role); err != nil {
		return err
	}
	for _, perm := range perms {
		if _, err := tx.Exec(ctx,
			`INSERT INTO role_permissions (role_name, permission) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			role, perm); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ListPermissions returns the role's declared permissions in alphabetical
// order. Returns an empty slice (not nil) if the role exists but has no
// rows in role_permissions. Returns ErrRoleNotFound if the role itself
// does not exist.
func (r *PGRoleRepository) ListPermissions(ctx context.Context, role string) ([]string, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM roles WHERE name = $1)`, role).
		Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrRoleNotFound
	}
	rows, err := r.pool.Query(ctx,
		`SELECT permission FROM role_permissions WHERE role_name = $1 ORDER BY permission`,
		role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

var _ RoleRepository = (*PGRoleRepository)(nil)

// roleRecordNow is used by the in-memory test fake for deterministic
// CreatedAt. Production callers go through the PG pool.
var roleRecordNow = func() time.Time { return time.Now() }
