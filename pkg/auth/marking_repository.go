package auth

import (
	"context"
	"errors"
	"time"

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
	// Marking rows on the read path. Grants whose expires_at has passed
	// are filtered out at the SQL layer so callers never observe expired
	// markings.
	GetUserMarkings(ctx context.Context, userID string) ([]string, error)

	// GrantMarking inserts a (user, marking) row. Idempotent: granting
	// the same marking to the same user twice updates the existing row
	// (notably expires_at) and does NOT return an error so admin tooling
	// can be safely re-run.
	//
	// expiresAt is the optional auto-revocation instant. A nil pointer
	// creates a permanent grant; a non-nil pointer binds the grant to
	// that timestamp. Passing a time in the past is legal but produces
	// an immediately-expired grant (the read path will never surface it).
	GrantMarking(ctx context.Context, userID, markingName, grantedBy string, expiresAt *time.Time) error

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
// empty when the user has no grants. Grants whose expires_at has passed
// are filtered out so the MarkingFilter never sees a stale grant.
func (r *PGMarkingRepository) GetUserMarkings(ctx context.Context, userID string) ([]string, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("marking repository: nil pool")
	}
	rows, err := r.pool.Query(ctx,
		`SELECT marking_name FROM user_markings
		 WHERE user_id = $1
		   AND (expires_at IS NULL OR expires_at > NOW())
		 ORDER BY marking_name`, userID)
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

// GrantMarking inserts a row idempotently. On conflict the row is updated
// so callers can re-grant with a fresh expires_at (or clear it) without a
// separate PATCH path. The granted_by column may be the empty string when
// the grant is being created from a non-admin context (e.g. bootstrap),
// in which case it is stored as NULL.
//
// expiresAt = nil creates (or refreshes to) a permanent grant; a non-nil
// pointer binds the grant to that timestamp. Re-granting a permanent
// marking on top of an expiring one clears the expires_at cleanly.
func (r *PGMarkingRepository) GrantMarking(ctx context.Context, userID, markingName, grantedBy string, expiresAt *time.Time) error {
	if r == nil || r.pool == nil {
		return errors.New("marking repository: nil pool")
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO user_markings (user_id, marking_name, granted_by, expires_at)
		 VALUES ($1, $2, NULLIF($3, ''), $4)
		 ON CONFLICT (user_id, marking_name) DO UPDATE
		   SET granted_by = EXCLUDED.granted_by,
		       expires_at = EXCLUDED.expires_at`,
		userID, markingName, grantedBy, expiresAt)
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

// ListGrantsByMarking satisfies MarkingGrantAdminRepository. Returns every
// non-expired (user, granted_at, granted_by, expires_at) row for the
// marking, ordered newest first so recent grants float to the top of the
// admin UI. Expired grants are filtered out at the SQL layer so the admin
// surface matches the enforcement surface. The slice is always non-nil;
// an unknown marking name returns [] rather than an error so the admin
// UI can distinguish "empty roster" from a lookup failure.
func (r *PGMarkingRepository) ListGrantsByMarking(ctx context.Context, markingName string) ([]MarkingGrant, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("marking repository: nil pool")
	}
	rows, err := r.pool.Query(ctx,
		`SELECT user_id, marking_name, granted_at, COALESCE(granted_by, ''), expires_at
		 FROM user_markings
		 WHERE marking_name = $1
		   AND (expires_at IS NULL OR expires_at > NOW())
		 ORDER BY granted_at DESC`, markingName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MarkingGrant, 0)
	for rows.Next() {
		var g MarkingGrant
		if err := rows.Scan(&g.UserID, &g.MarkingName, &g.GrantedAt, &g.GrantedBy, &g.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListGrantsByUser satisfies MarkingGrantAdminRepository. Returns every
// non-expired grant row held by userID, ordered alphabetically by marking
// name so the admin UI renders a stable list without sorting client-side.
// Expired grants are filtered out at the SQL layer. Always non-nil slice.
func (r *PGMarkingRepository) ListGrantsByUser(ctx context.Context, userID string) ([]MarkingGrant, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("marking repository: nil pool")
	}
	rows, err := r.pool.Query(ctx,
		`SELECT user_id, marking_name, granted_at, COALESCE(granted_by, ''), expires_at
		 FROM user_markings
		 WHERE user_id = $1
		   AND (expires_at IS NULL OR expires_at > NOW())
		 ORDER BY marking_name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MarkingGrant, 0)
	for rows.Next() {
		var g MarkingGrant
		if err := rows.Scan(&g.UserID, &g.MarkingName, &g.GrantedAt, &g.GrantedBy, &g.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}
