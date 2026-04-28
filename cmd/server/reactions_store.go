package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/reactions"
)

// pgReactionsStore satisfies reactions.Store by persisting rows to the
// reactions table (US-342). Lives in cmd/server/ rather than
// pkg/reactions/ so the package stays free of any pgx import — same
// dep-direction trick as pgWatchesStore / pgCommentsStore.
type pgReactionsStore struct {
	pool *pgxpool.Pool
}

func newPGReactionsStore(pool *pgxpool.Pool) *pgReactionsStore {
	return &pgReactionsStore{pool: pool}
}

// Create inserts a new reaction row. ON CONFLICT (user_id, target_rid,
// emoji) DO NOTHING keeps the call idempotent; the follow-up SELECT
// returns the canonical row so the caller always gets the original id
// and timestamp regardless of whether this call inserted or hit the
// existing row.
func (s *pgReactionsStore) Create(ctx context.Context, r *reactions.Reaction) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO reactions (id, user_id, target_rid, emoji, created_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, target_rid, emoji) DO NOTHING`,
		r.ID, r.UserID, r.TargetRID, r.Emoji,
	)
	if err != nil {
		return err
	}
	row := s.pool.QueryRow(ctx,
		`SELECT id::text, user_id, target_rid, emoji, created_at
		   FROM reactions
		  WHERE user_id = $1 AND target_rid = $2 AND emoji = $3`,
		r.UserID, r.TargetRID, r.Emoji,
	)
	var got reactions.Reaction
	if err := row.Scan(&got.ID, &got.UserID, &got.TargetRID, &got.Emoji, &got.CreatedAt); err != nil {
		return err
	}
	*r = got
	return nil
}

// Delete removes the (userID, targetRID, emoji) row. ErrNotFound when
// no row matches.
func (s *pgReactionsStore) Delete(ctx context.Context, userID, targetRID, emoji string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM reactions WHERE user_id = $1 AND target_rid = $2 AND emoji = $3`,
		userID, targetRID, emoji,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return reactions.ErrNotFound
	}
	return nil
}

// AggregateForTarget returns one bucket per distinct emoji on the
// target with the caller's "mine" flag set when (userID, target, emoji)
// has a row. Single GROUP BY query backed by the (target_rid, emoji)
// index from migration 000080. Buckets are ordered by descending count,
// then ascending emoji string for deterministic output.
func (s *pgReactionsStore) AggregateForTarget(ctx context.Context, userID, targetRID string) ([]reactions.EmojiCount, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT emoji,
		        COUNT(*)::int                                                 AS total,
		        BOOL_OR(user_id = $1)                                         AS mine
		   FROM reactions
		  WHERE target_rid = $2
		  GROUP BY emoji
		  ORDER BY total DESC, emoji ASC`,
		userID, targetRID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []reactions.EmojiCount
	for rows.Next() {
		var bucket reactions.EmojiCount
		if err := rows.Scan(&bucket.Emoji, &bucket.Count, &bucket.Mine); err != nil {
			return nil, err
		}
		out = append(out, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
