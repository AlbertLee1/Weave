package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/notifications"
)

// pgNotificationPreferenceStore is the Postgres-backed
// notifications.PreferenceStore wired in cmd/server. Same dep-direction
// trick as pgUserPrefsStore (US-350): pkg/notifications stays free of
// pgx imports, the adapter lives here.
type pgNotificationPreferenceStore struct {
	pool *pgxpool.Pool
}

func newPGNotificationPreferenceStore(pool *pgxpool.Pool) *pgNotificationPreferenceStore {
	if pool == nil {
		return nil
	}
	return &pgNotificationPreferenceStore{pool: pool}
}

func (s *pgNotificationPreferenceStore) ListByUser(ctx context.Context, userID string) ([]notifications.Preference, error) {
	if s == nil || s.pool == nil || userID == "" {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT user_id, channel, enabled, target, created_at, updated_at
		   FROM notification_preferences
		  WHERE user_id = $1
		  ORDER BY channel`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []notifications.Preference
	for rows.Next() {
		var p notifications.Preference
		if err := rows.Scan(&p.UserID, &p.Channel, &p.Enabled, &p.Target, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *pgNotificationPreferenceStore) Upsert(ctx context.Context, p *notifications.Preference) error {
	if s == nil || s.pool == nil || p == nil || p.UserID == "" || p.Channel == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO notification_preferences (user_id, channel, enabled, target, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, NOW(), NOW())
		 ON CONFLICT (user_id, channel) DO UPDATE
		   SET enabled    = EXCLUDED.enabled,
		       target     = EXCLUDED.target,
		       updated_at = NOW()`,
		p.UserID, p.Channel, p.Enabled, p.Target)
	return err
}

func (s *pgNotificationPreferenceStore) Delete(ctx context.Context, userID, channel string) error {
	if s == nil || s.pool == nil || userID == "" || channel == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`DELETE FROM notification_preferences WHERE user_id = $1 AND channel = $2`,
		userID, channel)
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

var _ notifications.PreferenceStore = (*pgNotificationPreferenceStore)(nil)
