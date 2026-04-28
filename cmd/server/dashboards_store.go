package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/dashboards"
)

// pgDashboardsStore satisfies dashboards.Store by persisting rows to
// the dashboards table (US-329). Lives in cmd/server/ rather than
// pkg/dashboards/ so the package stays free of any pgx import — same
// dep-direction trick as pgSavedSearchesStore.
type pgDashboardsStore struct {
	pool *pgxpool.Pool
}

func newPGDashboardsStore(pool *pgxpool.Pool) *pgDashboardsStore {
	return &pgDashboardsStore{pool: pool}
}

func isDashboardUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "duplicate key value")
}

// definitionForWrite normalises a JSONB-bound payload — pgx encodes a
// nil json.RawMessage as the string "null", which the column will
// accept but breaks the "absent ⇒ {}" round-trip. Mirrors the
// pgSavedSearchesStore.definitionForWrite shape.
func dashboardDefinitionForWrite(def json.RawMessage) []byte {
	if len(def) == 0 {
		return []byte("{}")
	}
	return []byte(def)
}

func (s *pgDashboardsStore) Create(ctx context.Context, row *dashboards.Dashboard) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO dashboards (id, name, created_by, is_public, definition)
		 VALUES ($1, $2, $3, $4, $5)`,
		row.ID, row.Name, row.CreatedBy, row.IsPublic,
		dashboardDefinitionForWrite(row.Definition),
	)
	if err != nil {
		if isDashboardUniqueViolation(err) {
			return dashboards.ErrNameConflict
		}
		return err
	}
	fresh, err := s.getRaw(ctx, row.ID)
	if err != nil {
		return err
	}
	*row = *fresh
	return nil
}

// getRaw fetches a row by id with no owner / public gate. Internal —
// callers reach the gated form through Get.
func (s *pgDashboardsStore) getRaw(ctx context.Context, id string) (*dashboards.Dashboard, error) {
	var row dashboards.Dashboard
	var defBytes []byte
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, name, created_by, is_public,
		        COALESCE(definition, '{}'::jsonb), created_at, updated_at
		 FROM dashboards WHERE id = $1`,
		id).
		Scan(&row.ID, &row.Name, &row.CreatedBy, &row.IsPublic,
			&defBytes, &row.CreatedAt, &row.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, dashboards.ErrNotFound
		}
		return nil, err
	}
	row.Definition = json.RawMessage(defBytes)
	return &row, nil
}

func (s *pgDashboardsStore) Get(ctx context.Context, id, createdBy string) (*dashboards.Dashboard, error) {
	row, err := s.getRaw(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.CreatedBy != createdBy && !row.IsPublic {
		return nil, dashboards.ErrNotFound
	}
	return row, nil
}

func (s *pgDashboardsStore) List(ctx context.Context, createdBy string) ([]*dashboards.Dashboard, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id::text, name, created_by, is_public,
		        COALESCE(definition, '{}'::jsonb), created_at, updated_at
		 FROM dashboards WHERE created_by = $1 ORDER BY name ASC`,
		createdBy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*dashboards.Dashboard
	for rows.Next() {
		var r dashboards.Dashboard
		var defBytes []byte
		if err := rows.Scan(&r.ID, &r.Name, &r.CreatedBy, &r.IsPublic,
			&defBytes, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Definition = json.RawMessage(defBytes)
		out = append(out, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *pgDashboardsStore) Update(ctx context.Context, id, createdBy string, upd dashboards.Update) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Name != nil {
		sets = append(sets, "name = $"+strconv.Itoa(argN))
		args = append(args, *upd.Name)
		argN++
	}
	if upd.Definition != nil {
		sets = append(sets, "definition = $"+strconv.Itoa(argN))
		args = append(args, dashboardDefinitionForWrite(*upd.Definition))
		argN++
	}
	if upd.IsPublic != nil {
		sets = append(sets, "is_public = $"+strconv.Itoa(argN))
		args = append(args, *upd.IsPublic)
		argN++
	}
	args = append(args, id, createdBy)
	tag, err := s.pool.Exec(ctx,
		`UPDATE dashboards SET `+strings.Join(sets, ", ")+
			` WHERE id = $`+strconv.Itoa(argN)+
			` AND created_by = $`+strconv.Itoa(argN+1),
		args...)
	if err != nil {
		if isDashboardUniqueViolation(err) {
			return dashboards.ErrNameConflict
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return dashboards.ErrNotFound
	}
	return nil
}

func (s *pgDashboardsStore) Delete(ctx context.Context, id, createdBy string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM dashboards WHERE id = $1 AND created_by = $2`,
		id, createdBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return dashboards.ErrNotFound
	}
	return nil
}
