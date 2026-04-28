package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/watches"
)

// pgWatchesStore satisfies watches.Store by persisting rows to the
// watches table (US-337). Lives in cmd/server/ rather than pkg/watches/
// so the package stays free of any pgx import — same dep-direction
// trick as pgCommentsStore / pgSavedSearchesStore.
type pgWatchesStore struct {
	pool *pgxpool.Pool
}

func newPGWatchesStore(pool *pgxpool.Pool) *pgWatchesStore {
	return &pgWatchesStore{pool: pool}
}

// Create inserts a new follow row. ON CONFLICT (user_id, target_rid) DO
// NOTHING keeps the call idempotent; we then SELECT the canonical row so
// the caller always receives the original id and timestamp.
func (s *pgWatchesStore) Create(ctx context.Context, w *watches.Watch) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO watches (id, user_id, target_rid, created_at)
		 VALUES ($1, $2, $3, NOW())
		 ON CONFLICT (user_id, target_rid) DO NOTHING`,
		w.ID, w.UserID, w.TargetRID,
	)
	if err != nil {
		return err
	}
	row := s.pool.QueryRow(ctx,
		`SELECT id::text, user_id, target_rid, created_at
		   FROM watches WHERE user_id = $1 AND target_rid = $2`,
		w.UserID, w.TargetRID,
	)
	var got watches.Watch
	if err := row.Scan(&got.ID, &got.UserID, &got.TargetRID, &got.CreatedAt); err != nil {
		return err
	}
	*w = got
	return nil
}

// Delete removes the (userID, targetRID) row.
func (s *pgWatchesStore) Delete(ctx context.Context, userID, targetRID string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM watches WHERE user_id = $1 AND target_rid = $2`,
		userID, targetRID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return watches.ErrNotFound
	}
	return nil
}

// List returns every row owned by userID, ordered most-recent first.
func (s *pgWatchesStore) List(ctx context.Context, userID string) ([]*watches.Watch, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, user_id, target_rid, created_at
		   FROM watches WHERE user_id = $1
		   ORDER BY created_at DESC, id ASC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*watches.Watch
	for rows.Next() {
		var w watches.Watch
		if err := rows.Scan(&w.ID, &w.UserID, &w.TargetRID, &w.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// IsWatching reports whether a row for (userID, targetRID) exists.
func (s *pgWatchesStore) IsWatching(ctx context.Context, userID, targetRID string) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT 1 FROM watches WHERE user_id = $1 AND target_rid = $2 LIMIT 1`,
		userID, targetRID,
	).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
