package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/aip"
)

// pgAIPToolCatalog satisfies aip.ToolCatalog by persisting ToolRecord
// rows into the aip_tools table (US-285). Lives in cmd/server/ to keep
// pkg/aip free of any pgx import — same dep trick as pgAIPStore +
// pgFeatureFlagsStore.
type pgAIPToolCatalog struct {
	pool *pgxpool.Pool
}

func newPGAIPToolCatalog(pool *pgxpool.Pool) *pgAIPToolCatalog {
	return &pgAIPToolCatalog{pool: pool}
}

func (s *pgAIPToolCatalog) CreateTool(ctx context.Context, t *aip.ToolRecord) error {
	if t == nil {
		return errors.New("aip: tool record is nil")
	}
	params := []byte(t.Parameters)
	if len(params) == 0 {
		params = []byte("{}")
	}
	err := s.pool.QueryRow(ctx,
		`INSERT INTO aip_tools (name, description, parameters, handler_function_rid, enabled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING created_at, updated_at`,
		t.Name, t.Description, params, t.HandlerFunctionRID, t.Enabled, t.CreatedBy,
	).Scan(&t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		if isAIPUniqueViolation(err) {
			return aip.ErrToolAlreadyExists
		}
		return err
	}
	return nil
}

func (s *pgAIPToolCatalog) GetTool(ctx context.Context, name string) (*aip.ToolRecord, error) {
	var rec aip.ToolRecord
	var paramsJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT name, description, COALESCE(parameters, '{}'::jsonb),
		        COALESCE(handler_function_rid, ''), enabled,
		        COALESCE(created_by, ''), created_at, updated_at
		 FROM aip_tools WHERE name = $1`, name).
		Scan(&rec.Name, &rec.Description, &paramsJSON,
			&rec.HandlerFunctionRID, &rec.Enabled,
			&rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, aip.ErrToolRecordNotFound
		}
		return nil, err
	}
	if len(paramsJSON) > 0 {
		rec.Parameters = json.RawMessage(paramsJSON)
	}
	return &rec, nil
}

func (s *pgAIPToolCatalog) ListTools(ctx context.Context) ([]*aip.ToolRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT name, description, COALESCE(parameters, '{}'::jsonb),
		        COALESCE(handler_function_rid, ''), enabled,
		        COALESCE(created_by, ''), created_at, updated_at
		 FROM aip_tools ORDER BY name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*aip.ToolRecord
	for rows.Next() {
		var rec aip.ToolRecord
		var paramsJSON []byte
		if err := rows.Scan(&rec.Name, &rec.Description, &paramsJSON,
			&rec.HandlerFunctionRID, &rec.Enabled,
			&rec.CreatedBy, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		if len(paramsJSON) > 0 {
			rec.Parameters = json.RawMessage(paramsJSON)
		}
		out = append(out, &rec)
	}
	return out, rows.Err()
}

func (s *pgAIPToolCatalog) UpdateTool(ctx context.Context, name string, upd aip.ToolUpdate) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Description != nil {
		sets = append(sets, "description = $"+strconv.Itoa(argN))
		args = append(args, *upd.Description)
		argN++
	}
	if upd.Parameters != nil {
		params := []byte(*upd.Parameters)
		if len(params) == 0 {
			params = []byte("{}")
		}
		sets = append(sets, "parameters = $"+strconv.Itoa(argN))
		args = append(args, params)
		argN++
	}
	if upd.HandlerFunctionRID != nil {
		sets = append(sets, "handler_function_rid = $"+strconv.Itoa(argN))
		args = append(args, *upd.HandlerFunctionRID)
		argN++
	}
	if upd.Enabled != nil {
		sets = append(sets, "enabled = $"+strconv.Itoa(argN))
		args = append(args, *upd.Enabled)
		argN++
	}
	args = append(args, name)
	tag, err := s.pool.Exec(ctx,
		`UPDATE aip_tools SET `+strings.Join(sets, ", ")+
			` WHERE name = $`+strconv.Itoa(argN), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return aip.ErrToolRecordNotFound
	}
	return nil
}

func (s *pgAIPToolCatalog) DeleteTool(ctx context.Context, name string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM aip_tools WHERE name = $1`, name)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return aip.ErrToolRecordNotFound
	}
	return nil
}
