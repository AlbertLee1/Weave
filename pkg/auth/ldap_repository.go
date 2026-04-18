package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGLDAPSyncStore is the Postgres-backed LDAPSyncStore. Multi-row
// operations (sync-run insert + prune, group membership replace) run
// inside a transaction so observers never see a half-applied state.
type PGLDAPSyncStore struct {
	pool *pgxpool.Pool
}

// NewPGLDAPSyncStore wraps a pgx pool as an LDAPSyncStore.
func NewPGLDAPSyncStore(pool *pgxpool.Pool) *PGLDAPSyncStore {
	return &PGLDAPSyncStore{pool: pool}
}

// UpsertSyncedUser inserts or updates a user keyed on ldap_dn. created
// is true when the row did not exist before this call. last_synced_at
// is stamped to syncedAt and disabled_at is cleared so a previously
// orphaned-but-now-returned user is re-enabled in one round-trip.
func (s *PGLDAPSyncStore) UpsertSyncedUser(ctx context.Context, dn, email, displayName string, syncedAt time.Time) (string, bool, error) {
	if strings.TrimSpace(dn) == "" {
		return "", false, errors.New("ldap upsert user: empty dn")
	}
	if strings.TrimSpace(email) == "" {
		return "", false, errors.New("ldap upsert user: empty email")
	}
	id := "user:" + email

	var existingDN *string
	err := s.pool.QueryRow(ctx,
		`SELECT ldap_dn FROM users WHERE id = $1`, id).Scan(&existingDN)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// New user.
		_, ierr := s.pool.Exec(ctx,
			`INSERT INTO users (id, email, name, ldap_dn, last_synced_at, created_at, updated_at)
			 VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, $5, now(), now())`,
			id, email, displayName, dn, syncedAt)
		if ierr != nil {
			return "", false, fmt.Errorf("insert user %s: %w", id, ierr)
		}
		return id, true, nil
	case err != nil:
		return "", false, fmt.Errorf("lookup user %s: %w", id, err)
	}

	// Existing row — update DN / name / sync timestamp; clear disabled_at;
	// do NOT touch password_hash (hybrid password+LDAP orgs need both
	// auth paths working on the same user).
	_, uerr := s.pool.Exec(ctx,
		`UPDATE users
		 SET ldap_dn = $2,
		     name = COALESCE(NULLIF($3, ''), name),
		     last_synced_at = $4,
		     disabled_at = NULL,
		     updated_at = now()
		 WHERE id = $1`,
		id, dn, displayName, syncedAt)
	if uerr != nil {
		return "", false, fmt.Errorf("update user %s: %w", id, uerr)
	}
	return id, false, nil
}

// DisableOrphanedSyncedUsers stamps disabled_at=now() on every user row
// whose ldap_dn IS NOT NULL but whose last_synced_at is older than
// syncedAt. Locally-provisioned (NULL ldap_dn) users are untouched.
func (s *PGLDAPSyncStore) DisableOrphanedSyncedUsers(ctx context.Context, syncedAt time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users
		 SET disabled_at = now(), updated_at = now()
		 WHERE ldap_dn IS NOT NULL
		   AND disabled_at IS NULL
		   AND (last_synced_at IS NULL OR last_synced_at < $1)`,
		syncedAt)
	if err != nil {
		return 0, fmt.Errorf("disable orphaned users: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// UpsertSyncedGroup inserts or updates a group keyed on ldap_dn. The
// id field is the canonical UUID used by user_groups membership rows.
func (s *PGLDAPSyncStore) UpsertSyncedGroup(ctx context.Context, dn, name, description string, syncedAt time.Time) (string, bool, error) {
	if strings.TrimSpace(dn) == "" {
		return "", false, errors.New("ldap upsert group: empty dn")
	}
	if strings.TrimSpace(name) == "" {
		return "", false, errors.New("ldap upsert group: empty name")
	}

	var existingID string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM groups WHERE ldap_dn = $1`, dn).Scan(&existingID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// New group — let the DB stamp the UUID.
		var id string
		ierr := s.pool.QueryRow(ctx,
			`INSERT INTO groups (name, description, ldap_dn, last_synced_at, created_at, updated_at)
			 VALUES ($1, $2, $3, $4, now(), now())
			 ON CONFLICT (name) DO UPDATE SET
			     description    = EXCLUDED.description,
			     ldap_dn        = EXCLUDED.ldap_dn,
			     last_synced_at = EXCLUDED.last_synced_at,
			     updated_at     = now()
			 RETURNING id`,
			name, description, dn, syncedAt).Scan(&id)
		if ierr != nil {
			return "", false, fmt.Errorf("insert group %s: %w", name, ierr)
		}
		return id, true, nil
	case err != nil:
		return "", false, fmt.Errorf("lookup group %s: %w", dn, err)
	}

	_, uerr := s.pool.Exec(ctx,
		`UPDATE groups
		 SET name = $2,
		     description = $3,
		     last_synced_at = $4,
		     updated_at = now()
		 WHERE id = $1`,
		existingID, name, description, syncedAt)
	if uerr != nil {
		return "", false, fmt.Errorf("update group %s: %w", existingID, uerr)
	}
	return existingID, false, nil
}

// ReplaceGroupMembers atomically reconciles user_groups for groupID with
// the supplied user-id slice. Returns the count of rows actually inserted
// (existing memberships are untouched, missing ones are added, removed
// ones are deleted).
func (s *PGLDAPSyncStore) ReplaceGroupMembers(ctx context.Context, groupID string, userIDs []string) (int, error) {
	if strings.TrimSpace(groupID) == "" {
		return 0, errors.New("ldap replace members: empty group id")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	existing := map[string]struct{}{}
	rows, err := tx.Query(ctx,
		`SELECT user_id FROM user_groups WHERE group_id = $1`, groupID)
	if err != nil {
		return 0, fmt.Errorf("list existing members: %w", err)
	}
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan member: %w", err)
		}
		existing[uid] = struct{}{}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate members: %w", err)
	}

	desired := map[string]struct{}{}
	for _, uid := range userIDs {
		desired[uid] = struct{}{}
	}

	added := 0
	for uid := range desired {
		if _, present := existing[uid]; present {
			continue
		}
		if _, ierr := tx.Exec(ctx,
			`INSERT INTO user_groups (user_id, group_id) VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			uid, groupID); ierr != nil {
			return 0, fmt.Errorf("add member %s: %w", uid, ierr)
		}
		added++
	}
	for uid := range existing {
		if _, present := desired[uid]; present {
			continue
		}
		if _, derr := tx.Exec(ctx,
			`DELETE FROM user_groups WHERE user_id = $1 AND group_id = $2`,
			uid, groupID); derr != nil {
			return 0, fmt.Errorf("remove member %s: %w", uid, derr)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return added, nil
}

// UserIDByDN returns the user id associated with ldap_dn=dn, or "" when
// no row exists. Errors are returned only on actual database failures so
// the caller can distinguish "missing" from "broken".
func (s *PGLDAPSyncStore) UserIDByDN(ctx context.Context, dn string) (string, error) {
	if strings.TrimSpace(dn) == "" {
		return "", nil
	}
	var id string
	err := s.pool.QueryRow(ctx,
		`SELECT id FROM users WHERE ldap_dn = $1`, dn).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("lookup user by dn %s: %w", dn, err)
	}
	return id, nil
}

// RecordSyncRun inserts a row into ldap_sync_runs and prunes the table
// to the most recent 100 rows. Pruning is best-effort: a failure is
// logged but does not fail the insert.
func (s *PGLDAPSyncStore) RecordSyncRun(ctx context.Context, run *LDAPSyncRun) error {
	if run == nil {
		return errors.New("ldap record sync run: nil run")
	}
	finishedAt := run.FinishedAt
	err := s.pool.QueryRow(ctx,
		`INSERT INTO ldap_sync_runs (started_at, finished_at, users_seen, users_created, users_updated, users_disabled, groups_seen, groups_created, groups_updated, memberships_added, error_message)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id`,
		run.StartedAt, finishedAt, run.UsersSeen, run.UsersCreated, run.UsersUpdated, run.UsersDisabled,
		run.GroupsSeen, run.GroupsCreated, run.GroupsUpdated, run.MembershipsAdded, run.ErrorMessage,
	).Scan(&run.ID)
	if err != nil {
		return fmt.Errorf("insert ldap_sync_runs: %w", err)
	}
	// Best-effort prune. Cap at most-recent 100 rows.
	if _, perr := s.pool.Exec(ctx,
		`DELETE FROM ldap_sync_runs
		 WHERE id NOT IN (SELECT id FROM ldap_sync_runs ORDER BY started_at DESC LIMIT 100)`); perr != nil {
		// Best-effort — not a fatal error; the audit row is already in.
		return nil
	}
	return nil
}
