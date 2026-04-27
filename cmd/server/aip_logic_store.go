package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/aip/logic"
)

// pgAIPLogicStore satisfies logic.Store by persisting flow + flow-run
// rows into the aip_logic_flows / aip_logic_flow_runs tables (US-281).
// Lives in cmd/server to keep pkg/aip/logic free of any pgx import.
type pgAIPLogicStore struct {
	pool *pgxpool.Pool
}

func newPGAIPLogicStore(pool *pgxpool.Pool) *pgAIPLogicStore {
	return &pgAIPLogicStore{pool: pool}
}

func isLogicUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value")
}

func (s *pgAIPLogicStore) CreateFlow(ctx context.Context, f *logic.Flow) error {
	nodes, err := json.Marshal(f.Nodes)
	if err != nil {
		return err
	}
	edges, err := json.Marshal(f.Edges)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx,
		`INSERT INTO aip_logic_flows (id, name, description, nodes, edges, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		f.ID, f.Name, f.Description, nodes, edges, f.CreatedBy,
	); err != nil {
		if isLogicUniqueViolation(err) {
			return logic.ErrFlowAlreadyExists
		}
		return err
	}
	fresh, err := s.GetFlow(ctx, f.ID)
	if err != nil {
		return err
	}
	*f = *fresh
	return nil
}

func (s *pgAIPLogicStore) GetFlow(ctx context.Context, id string) (*logic.Flow, error) {
	var (
		f         logic.Flow
		nodesRaw  []byte
		edgesRaw  []byte
		createdBy string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, description, nodes, edges, COALESCE(created_by,''),
		        created_at, updated_at
		 FROM aip_logic_flows WHERE id = $1`, id).
		Scan(&f.ID, &f.Name, &f.Description, &nodesRaw, &edgesRaw,
			&createdBy, &f.CreatedAt, &f.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, logic.ErrFlowNotFound
		}
		return nil, err
	}
	f.CreatedBy = createdBy
	if err := json.Unmarshal(coalesceJSON(nodesRaw, "[]"), &f.Nodes); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(coalesceJSON(edgesRaw, "[]"), &f.Edges); err != nil {
		return nil, err
	}
	return &f, nil
}

func (s *pgAIPLogicStore) ListFlows(ctx context.Context, createdBy string) ([]*logic.Flow, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if createdBy == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, name, description, nodes, edges, COALESCE(created_by,''),
			        created_at, updated_at
			 FROM aip_logic_flows ORDER BY created_at DESC, id ASC`)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, name, description, nodes, edges, COALESCE(created_by,''),
			        created_at, updated_at
			 FROM aip_logic_flows WHERE created_by = $1
			 ORDER BY created_at DESC, id ASC`, createdBy)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*logic.Flow
	for rows.Next() {
		var (
			f        logic.Flow
			nodesRaw []byte
			edgesRaw []byte
		)
		if err := rows.Scan(&f.ID, &f.Name, &f.Description, &nodesRaw, &edgesRaw,
			&f.CreatedBy, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(coalesceJSON(nodesRaw, "[]"), &f.Nodes); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(coalesceJSON(edgesRaw, "[]"), &f.Edges); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, rows.Err()
}

func (s *pgAIPLogicStore) UpdateFlow(ctx context.Context, id string, upd logic.FlowUpdate) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Name != nil {
		sets = append(sets, "name = $"+strconv.Itoa(argN))
		args = append(args, *upd.Name)
		argN++
	}
	if upd.Description != nil {
		sets = append(sets, "description = $"+strconv.Itoa(argN))
		args = append(args, *upd.Description)
		argN++
	}
	if upd.Nodes != nil {
		nodes, err := json.Marshal(*upd.Nodes)
		if err != nil {
			return err
		}
		sets = append(sets, "nodes = $"+strconv.Itoa(argN))
		args = append(args, nodes)
		argN++
	}
	if upd.Edges != nil {
		edges, err := json.Marshal(*upd.Edges)
		if err != nil {
			return err
		}
		sets = append(sets, "edges = $"+strconv.Itoa(argN))
		args = append(args, edges)
		argN++
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx,
		`UPDATE aip_logic_flows SET `+strings.Join(sets, ", ")+
			` WHERE id = $`+strconv.Itoa(argN), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return logic.ErrFlowNotFound
	}
	return nil
}

func (s *pgAIPLogicStore) DeleteFlow(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM aip_logic_flows WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return logic.ErrFlowNotFound
	}
	return nil
}

func (s *pgAIPLogicStore) AppendRun(ctx context.Context, run *logic.Run) error {
	input, err := json.Marshal(coalesceMap(run.Input))
	if err != nil {
		return err
	}
	output, err := json.Marshal(coalesceMap(run.Output))
	if err != nil {
		return err
	}
	traceJSON, err := json.Marshal(coalesceTrace(run.Trace))
	if err != nil {
		return err
	}
	var id int64
	createdAt := run.CreatedAt
	err = s.pool.QueryRow(ctx,
		`INSERT INTO aip_logic_flow_runs
		   (flow_id, status, input, output, trace, error_message, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at`,
		run.FlowID, run.Status, input, output, traceJSON, run.Error, run.CreatedBy).
		Scan(&id, &createdAt)
	if err != nil {
		if strings.Contains(err.Error(), "violates foreign key constraint") {
			return logic.ErrFlowNotFound
		}
		return err
	}
	run.ID = id
	run.CreatedAt = createdAt
	return nil
}

func (s *pgAIPLogicStore) ListRuns(ctx context.Context, flowID string, limit int) ([]*logic.Run, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM aip_logic_flows WHERE id = $1)`, flowID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, logic.ErrFlowNotFound
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, flow_id, status, input, output, trace, error_message,
		        COALESCE(created_by,''), created_at
		 FROM aip_logic_flow_runs WHERE flow_id = $1
		 ORDER BY id DESC LIMIT $2`, flowID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*logic.Run
	for rows.Next() {
		var (
			r          logic.Run
			input      []byte
			output     []byte
			traceBytes []byte
		)
		if err := rows.Scan(&r.ID, &r.FlowID, &r.Status, &input, &output, &traceBytes,
			&r.Error, &r.CreatedBy, &r.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(coalesceJSON(input, "{}"), &r.Input)
		_ = json.Unmarshal(coalesceJSON(output, "{}"), &r.Output)
		_ = json.Unmarshal(coalesceJSON(traceBytes, "[]"), &r.Trace)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// coalesceJSON returns def when raw is nil/empty; otherwise raw. Some
// JSONB columns can come back as nil for legacy rows; encoders treat
// "null" as a valid value but the resulting Go nil map is harder to
// reason about than an empty literal.
func coalesceJSON(raw []byte, def string) []byte {
	if len(raw) == 0 {
		return []byte(def)
	}
	return raw
}

// coalesceMap returns m when non-nil, else an empty map so the JSONB
// column never contains the SQL string "null".
func coalesceMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// coalesceTrace returns t when non-nil, else an empty slice.
func coalesceTrace(t []logic.TraceEntry) []logic.TraceEntry {
	if t == nil {
		return []logic.TraceEntry{}
	}
	return t
}
