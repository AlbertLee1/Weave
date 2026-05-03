package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/quiver"
)

// pgQuiverStore satisfies quiver.Store by persisting rows to the
// quiver_dashboards table (US-403). Lives in cmd/server/ rather than
// pkg/quiver/ so the package stays free of any pgx import — same
// dep-direction trick as pgDashboardsStore.
type pgQuiverStore struct {
	pool *pgxpool.Pool
}

func newPGQuiverStore(pool *pgxpool.Pool) *pgQuiverStore {
	return &pgQuiverStore{pool: pool}
}

func isQuiverUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "duplicate key value")
}

// quiverConfigForWrite normalises a JSONB-bound payload — pgx encodes a
// nil json.RawMessage as the string "null", which the column will
// accept but breaks the "absent ⇒ {}" round-trip.
func quiverConfigForWrite(cfg json.RawMessage) []byte {
	if len(cfg) == 0 {
		return []byte("{}")
	}
	return []byte(cfg)
}

func (s *pgQuiverStore) Save(ctx context.Context, row *quiver.Dashboard) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO quiver_dashboards (rid, name, owner, config_json)
		 VALUES ($1, $2, $3, $4)`,
		row.RID, row.Name, row.Owner,
		quiverConfigForWrite(row.Config),
	)
	if err != nil {
		if isQuiverUniqueViolation(err) {
			return quiver.ErrNameConflict
		}
		return err
	}
	fresh, err := s.getRaw(ctx, row.RID)
	if err != nil {
		return err
	}
	*row = *fresh
	return nil
}

// getRaw fetches a row by RID with no owner gate. Internal — callers
// reach the gated form through Get; the share-link surface uses
// GetByRID directly.
func (s *pgQuiverStore) getRaw(ctx context.Context, rid string) (*quiver.Dashboard, error) {
	var row quiver.Dashboard
	var cfgBytes []byte
	err := s.pool.QueryRow(ctx,
		`SELECT rid, name, owner,
		        COALESCE(config_json, '{}'::jsonb), created_at, updated_at
		 FROM quiver_dashboards WHERE rid = $1`,
		rid).
		Scan(&row.RID, &row.Name, &row.Owner,
			&cfgBytes, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, quiver.ErrNotFound
		}
		return nil, err
	}
	row.Config = json.RawMessage(cfgBytes)
	return &row, nil
}

func (s *pgQuiverStore) Get(ctx context.Context, rid, owner string) (*quiver.Dashboard, error) {
	row, err := s.getRaw(ctx, rid)
	if err != nil {
		return nil, err
	}
	if row.Owner != owner {
		return nil, quiver.ErrNotFound
	}
	return row, nil
}

func (s *pgQuiverStore) GetByRID(ctx context.Context, rid string) (*quiver.Dashboard, error) {
	return s.getRaw(ctx, rid)
}

func (s *pgQuiverStore) List(ctx context.Context, owner string) ([]*quiver.Dashboard, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT rid, name, owner,
		        COALESCE(config_json, '{}'::jsonb), created_at, updated_at
		 FROM quiver_dashboards WHERE owner = $1 ORDER BY name ASC`,
		owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*quiver.Dashboard
	for rows.Next() {
		var r quiver.Dashboard
		var cfgBytes []byte
		if err := rows.Scan(&r.RID, &r.Name, &r.Owner,
			&cfgBytes, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Config = json.RawMessage(cfgBytes)
		out = append(out, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *pgQuiverStore) Update(ctx context.Context, rid, owner string, upd quiver.Update) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Name != nil {
		sets = append(sets, "name = $"+strconv.Itoa(argN))
		args = append(args, *upd.Name)
		argN++
	}
	if upd.Config != nil {
		sets = append(sets, "config_json = $"+strconv.Itoa(argN))
		args = append(args, quiverConfigForWrite(*upd.Config))
		argN++
	}
	args = append(args, rid, owner)
	tag, err := s.pool.Exec(ctx,
		`UPDATE quiver_dashboards SET `+strings.Join(sets, ", ")+
			` WHERE rid = $`+strconv.Itoa(argN)+
			` AND owner = $`+strconv.Itoa(argN+1),
		args...)
	if err != nil {
		if isQuiverUniqueViolation(err) {
			return quiver.ErrNameConflict
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return quiver.ErrNotFound
	}
	return nil
}

func (s *pgQuiverStore) Delete(ctx context.Context, rid, owner string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM quiver_dashboards WHERE rid = $1 AND owner = $2`,
		rid, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return quiver.ErrNotFound
	}
	return nil
}
