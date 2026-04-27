package main

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/featureflags"
)

// isFeatureFlagUniqueViolation reports whether err is a PostgreSQL
// unique-index violation (SQLSTATE 23505). Kept inline rather than
// importing pgconn so the cmd/server binary doesn't pull in an extra
// driver package path. Same shape as auth.isUniqueViolation.
func isFeatureFlagUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "duplicate key value")
}

// pgFeatureFlagsStore satisfies featureflags.Store by persisting flag
// rows to the feature_flags table (US-276). Lives in cmd/server/
// rather than pkg/featureflags/ so the package stays free of any pgx
// import — same dep-direction trick as pgGDPRJobStore.
type pgFeatureFlagsStore struct {
	pool *pgxpool.Pool
}

func newPGFeatureFlagsStore(pool *pgxpool.Pool) *pgFeatureFlagsStore {
	return &pgFeatureFlagsStore{pool: pool}
}

func normaliseStringSlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func (s *pgFeatureFlagsStore) CreateFlag(ctx context.Context, flag *featureflags.Flag) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO feature_flags (name, description, enabled, realms, users)
		 VALUES ($1, $2, $3, $4, $5)`,
		flag.Name, flag.Description, flag.Enabled,
		normaliseStringSlice(flag.Realms),
		normaliseStringSlice(flag.Users),
	)
	if err != nil {
		if isFeatureFlagUniqueViolation(err) {
			return featureflags.ErrFlagAlreadyExists
		}
		return err
	}
	// Round-trip to pick up stamped timestamps.
	fresh, err := s.GetFlag(ctx, flag.Name)
	if err != nil {
		return err
	}
	*flag = *fresh
	return nil
}

func (s *pgFeatureFlagsStore) GetFlag(ctx context.Context, name string) (*featureflags.Flag, error) {
	var flag featureflags.Flag
	err := s.pool.QueryRow(ctx,
		`SELECT name, description, enabled,
		        COALESCE(realms, '{}'::text[]),
		        COALESCE(users, '{}'::text[]),
		        created_at, updated_at
		 FROM feature_flags WHERE name = $1`, name).
		Scan(&flag.Name, &flag.Description, &flag.Enabled,
			&flag.Realms, &flag.Users,
			&flag.CreatedAt, &flag.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, featureflags.ErrFlagNotFound
		}
		return nil, err
	}
	return &flag, nil
}

func (s *pgFeatureFlagsStore) ListFlags(ctx context.Context) ([]*featureflags.Flag, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT name, description, enabled,
		        COALESCE(realms, '{}'::text[]),
		        COALESCE(users, '{}'::text[]),
		        created_at, updated_at
		 FROM feature_flags ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*featureflags.Flag
	for rows.Next() {
		var f featureflags.Flag
		if err := rows.Scan(&f.Name, &f.Description, &f.Enabled,
			&f.Realms, &f.Users, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *pgFeatureFlagsStore) UpdateFlag(ctx context.Context, name string, upd featureflags.FlagUpdate) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Description != nil {
		sets = append(sets, "description = $"+strconv.Itoa(argN))
		args = append(args, *upd.Description)
		argN++
	}
	if upd.Enabled != nil {
		sets = append(sets, "enabled = $"+strconv.Itoa(argN))
		args = append(args, *upd.Enabled)
		argN++
	}
	if upd.Realms != nil {
		sets = append(sets, "realms = $"+strconv.Itoa(argN))
		args = append(args, normaliseStringSlice(*upd.Realms))
		argN++
	}
	if upd.Users != nil {
		sets = append(sets, "users = $"+strconv.Itoa(argN))
		args = append(args, normaliseStringSlice(*upd.Users))
		argN++
	}
	args = append(args, name)
	tag, err := s.pool.Exec(ctx,
		`UPDATE feature_flags SET `+strings.Join(sets, ", ")+
			` WHERE name = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return featureflags.ErrFlagNotFound
	}
	return nil
}

func (s *pgFeatureFlagsStore) DeleteFlag(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM feature_flags WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return featureflags.ErrFlagNotFound
	}
	return nil
}
