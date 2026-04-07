package auth

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MarkingRepository is the persistence interface for the markings and
// user_markings tables. It is intentionally narrow: the request hot path
// only ever needs GetUserMarkings, and the admin path only needs the
// other four. Adding cache wrappers around this interface is the
// preferred extension point if marking checks ever show up in profiles.
type MarkingRepository interface {
	// ListMarkings returns every marking definition in the markings
	// table, including the seeded standard set.
	ListMarkings(ctx context.Context) ([]Marking, error)

	// GetUserMarkings returns the marking *names* held by a user. The
	// MarkingFilter only needs the names, so we avoid materialising full
	// Marking rows on the read path.
	GetUserMarkings(ctx context.Context, userID string) ([]string, error)

	// GrantMarking inserts a (user, marking) row. Idempotent: granting
	// the same marking to the same user twice is a no-op and does NOT
	// return an error so admin tooling can be safely re-run.
	GrantMarking(ctx context.Context, userID, markingName, grantedBy string) error

	// RevokeMarking removes a (user, marking) row. Idempotent: revoking
	// a grant that does not exist returns nil so admin double-clicks are
	// safe.
	RevokeMarking(ctx context.Context, userID, markingName string) error
}

// PGMarkingRepository is the Postgres implementation of MarkingRepository.
// It is goroutine-safe; the underlying pgxpool handle is shared.
type PGMarkingRepository struct {
	pool *pgxpool.Pool
}

// NewPGMarkingRepository wraps a pgx pool as a MarkingRepository.
func NewPGMarkingRepository(pool *pgxpool.Pool) *PGMarkingRepository {
	return &PGMarkingRepository{pool: pool}
}

// ListMarkings reads every row in the markings table. The volume is
// expected to be tiny (single digits in practice) so a full scan is fine
// and there is no LIMIT clause.
func (r *PGMarkingRepository) ListMarkings(ctx context.Context) ([]Marking, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("marking repository: nil pool")
	}
	rows, err := r.pool.Query(ctx,
		`SELECT name, display_name, COALESCE(description, ''), COALESCE(color, ''), created_at
		 FROM markings
		 ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Marking
	for rows.Next() {
		var m Marking
		if err := rows.Scan(&m.Name, &m.DisplayName, &m.Description, &m.Color, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// GetUserMarkings returns the marking names a user holds. The slice is
// always non-nil (callers can range without nil-guarding) but may be
// empty when the user has no grants.
func (r *PGMarkingRepository) GetUserMarkings(ctx context.Context, userID string) ([]string, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("marking repository: nil pool")
	}
	rows, err := r.pool.Query(ctx,
		`SELECT marking_name FROM user_markings WHERE user_id = $1 ORDER BY marking_name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// GrantMarking inserts a row idempotently via ON CONFLICT DO NOTHING.
// The granted_by column may be the empty string when the grant is being
// created from a non-admin context (e.g. bootstrap), in which case it is
// stored as NULL.
func (r *PGMarkingRepository) GrantMarking(ctx context.Context, userID, markingName, grantedBy string) error {
	if r == nil || r.pool == nil {
		return errors.New("marking repository: nil pool")
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_markings (user_id, marking_name, granted_by)
		 VALUES ($1, $2, NULLIF($3, ''))
		 ON CONFLICT (user_id, marking_name) DO NOTHING`,
		userID, markingName, grantedBy)
	return err
}

// RevokeMarking deletes a (user, marking) row. The DELETE is naturally
// idempotent — deleting a row that does not exist returns 0 rows
// affected with no error — so callers can re-revoke without checking.
func (r *PGMarkingRepository) RevokeMarking(ctx context.Context, userID, markingName string) error {
	if r == nil || r.pool == nil {
		return errors.New("marking repository: nil pool")
	}
	_, err := r.pool.Exec(ctx,
		`DELETE FROM user_markings WHERE user_id = $1 AND marking_name = $2`,
		userID, markingName)
	return err
}
