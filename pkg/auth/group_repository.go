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

// ErrGroupNotFound is returned when a Group lookup by id returns no rows.
var ErrGroupNotFound = errors.New("group not found")

// ErrGroupNameConflict is returned when a Create or Update violates the
// unique-name invariant on the groups table.
var ErrGroupNameConflict = errors.New("group name already in use")

// GroupRepository is the narrow persistence surface for US-251.
//
//   - Create        inserts a new row and stamps ID + timestamps
//   - GetByID       returns the row by primary key
//   - GetByName     returns the row by unique name, or ErrGroupNotFound
//   - List          returns all groups, newest first
//   - Update        applies a partial PATCH (see GroupUpdate)
//   - Delete        hard-deletes the group; cascades into user_groups
//
// Membership operations:
//
//   - AddMember     idempotent insert into user_groups
//   - RemoveMember  idempotent delete (no error if not a member)
//   - ListMembers   returns user ids in the group
//   - ListUserGroups returns group ids the user belongs to
type GroupRepository interface {
	Create(ctx context.Context, g *Group) error
	GetByID(ctx context.Context, id string) (*Group, error)
	GetByName(ctx context.Context, name string) (*Group, error)
	List(ctx context.Context) ([]*Group, error)
	Update(ctx context.Context, id string, upd GroupUpdate) (*Group, error)
	Delete(ctx context.Context, id string) error

	AddMember(ctx context.Context, groupID, userID string) error
	RemoveMember(ctx context.Context, groupID, userID string) error
	ListMembers(ctx context.Context, groupID string) ([]string, error)
	ListUserGroups(ctx context.Context, userID string) ([]string, error)
}

// GroupUpdate is the partial-PATCH shape accepted by Update.
//
// Nil fields are preserved; non-nil fields are written regardless of value.
// Mirrors ServiceAccountUpdate / UpdateLinkTypeRequest pointer convention.
type GroupUpdate struct {
	Name        *string
	Description *string
}

// PGGroupRepository is the Postgres-backed GroupRepository.
type PGGroupRepository struct {
	pool *pgxpool.Pool
}

// NewPGGroupRepository wraps a pgx pool as a GroupRepository.
func NewPGGroupRepository(pool *pgxpool.Pool) *PGGroupRepository {
	return &PGGroupRepository{pool: pool}
}

const groupColumns = `id, name, description, created_at, updated_at`

// Create persists a new group. Caller must populate Name; everything else
// is optional. ID / CreatedAt / UpdatedAt are stamped by the DB and copied
// back onto the supplied record.
func (r *PGGroupRepository) Create(ctx context.Context, g *Group) error {
	if g == nil {
		return errors.New("group: nil record")
	}
	if g.Name == "" {
		return errors.New("group: name is required")
	}
	row := r.pool.QueryRow(ctx,
		`INSERT INTO groups (name, description)
		 VALUES ($1, $2)
		 RETURNING id, created_at, updated_at`,
		g.Name, g.Description)
	if err := row.Scan(&g.ID, &g.CreatedAt, &g.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return ErrGroupNameConflict
		}
		return err
	}
	return nil
}

func scanGroup(row pgx.Row) (*Group, error) {
	g := &Group{}
	err := row.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return g, nil
}

// GetByID returns the group by primary key.
func (r *PGGroupRepository) GetByID(ctx context.Context, id string) (*Group, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+groupColumns+` FROM groups WHERE id = $1`, id)
	g, err := scanGroup(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGroupNotFound
		}
		return nil, err
	}
	return g, nil
}

// GetByName returns the group by its unique name.
func (r *PGGroupRepository) GetByName(ctx context.Context, name string) (*Group, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+groupColumns+` FROM groups WHERE name = $1`, name)
	g, err := scanGroup(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrGroupNotFound
		}
		return nil, err
	}
	return g, nil
}

// List returns every group, newest first. v1 does not paginate — the
// expected cardinality is small (tens to low-hundreds of groups per org).
func (r *PGGroupRepository) List(ctx context.Context) ([]*Group, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+groupColumns+` FROM groups ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Group
	for rows.Next() {
		g, err := scanGroup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// Update applies a partial PATCH.
func (r *PGGroupRepository) Update(ctx context.Context, id string, upd GroupUpdate) (*Group, error) {
	sets := []string{}
	args := []interface{}{}
	if upd.Name != nil {
		sets = append(sets, "name = $"+strconv.Itoa(len(args)+1))
		args = append(args, *upd.Name)
	}
	if upd.Description != nil {
		sets = append(sets, "description = $"+strconv.Itoa(len(args)+1))
		args = append(args, *upd.Description)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	sql := `UPDATE groups SET ` + strings.Join(sets, ", ") +
		` WHERE id = $` + strconv.Itoa(len(args))
	ct, err := r.pool.Exec(ctx, sql, args...)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrGroupNameConflict
		}
		return nil, err
	}
	if ct.RowsAffected() == 0 {
		return nil, ErrGroupNotFound
	}
	return r.GetByID(ctx, id)
}

// Delete removes the group. Membership rows cascade via ON DELETE CASCADE.
func (r *PGGroupRepository) Delete(ctx context.Context, id string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM groups WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrGroupNotFound
	}
	return nil
}

// AddMember inserts a user into the group. Idempotent: re-adding an
// existing member is a no-op.
func (r *PGGroupRepository) AddMember(ctx context.Context, groupID, userID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_groups (user_id, group_id) VALUES ($1, $2)
		 ON CONFLICT (user_id, group_id) DO NOTHING`,
		userID, groupID)
	return err
}

// RemoveMember removes a user from the group. Idempotent: removing a
// non-member is a no-op.
func (r *PGGroupRepository) RemoveMember(ctx context.Context, groupID, userID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM user_groups WHERE user_id = $1 AND group_id = $2`,
		userID, groupID)
	return err
}

// ListMembers returns the user ids that belong to a group, joined-at DESC.
func (r *PGGroupRepository) ListMembers(ctx context.Context, groupID string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT user_id FROM user_groups WHERE group_id = $1 ORDER BY joined_at DESC`,
		groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

// ListUserGroups returns the group ids the user belongs to.
func (r *PGGroupRepository) ListUserGroups(ctx context.Context, userID string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT group_id FROM user_groups WHERE user_id = $1 ORDER BY joined_at DESC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var gid string
		if err := rows.Scan(&gid); err != nil {
			return nil, err
		}
		out = append(out, gid)
	}
	return out, rows.Err()
}

// _ enforces at compile time that PGGroupRepository implements GroupRepository.
var _ GroupRepository = (*PGGroupRepository)(nil)

// groupRecordNow is used by the in-memory test fake for deterministic
// CreatedAt / UpdatedAt. Production callers go through the PG pool and use
// the server clock.
var groupRecordNow = func() time.Time { return time.Now() }
