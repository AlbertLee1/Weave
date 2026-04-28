package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/pipeline"
)

// pgPipelineStore satisfies pipeline.Store by persisting Pipeline rows
// into the pipelines table (US-287). Lives in cmd/server to keep
// pkg/pipeline free of any pgx import — same dep trick as
// pgAIPLogicStore + pgAIPStore.
type pgPipelineStore struct {
	pool *pgxpool.Pool
}

func newPGPipelineStore(pool *pgxpool.Pool) *pgPipelineStore {
	return &pgPipelineStore{pool: pool}
}

func isPipelineUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value")
}

func (s *pgPipelineStore) CreatePipeline(ctx context.Context, p *pipeline.Pipeline) error {
	if p == nil {
		return errors.New("pipeline: pipeline is nil")
	}
	inputs, err := json.Marshal(coalesceInputs(p.Inputs))
	if err != nil {
		return err
	}
	transforms, err := json.Marshal(coalesceTransforms(p.Transforms))
	if err != nil {
		return err
	}
	outputs, err := json.Marshal(coalesceOutputs(p.Outputs))
	if err != nil {
		return err
	}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO pipelines
		   (id, name, description, inputs, transforms, outputs, schedule, enabled, created_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING created_at, updated_at`,
		p.ID, p.Name, p.Description, inputs, transforms, outputs,
		p.Schedule, p.Enabled, p.CreatedBy,
	).Scan(&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if isPipelineUniqueViolation(err) {
			return pipeline.ErrPipelineAlreadyExists
		}
		return err
	}
	return nil
}

func (s *pgPipelineStore) GetPipeline(ctx context.Context, id string) (*pipeline.Pipeline, error) {
	var (
		p             pipeline.Pipeline
		inputsRaw     []byte
		transformsRaw []byte
		outputsRaw    []byte
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, description,
		        COALESCE(inputs, '[]'::jsonb),
		        COALESCE(transforms, '[]'::jsonb),
		        COALESCE(outputs, '[]'::jsonb),
		        COALESCE(schedule, ''), enabled, COALESCE(created_by, ''),
		        created_at, updated_at
		 FROM pipelines WHERE id = $1`, id).
		Scan(&p.ID, &p.Name, &p.Description,
			&inputsRaw, &transformsRaw, &outputsRaw,
			&p.Schedule, &p.Enabled, &p.CreatedBy,
			&p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pipeline.ErrPipelineNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(coalesceJSON(inputsRaw, "[]"), &p.Inputs); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(coalesceJSON(transformsRaw, "[]"), &p.Transforms); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(coalesceJSON(outputsRaw, "[]"), &p.Outputs); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *pgPipelineStore) ListPipelines(ctx context.Context, createdBy string) ([]*pipeline.Pipeline, error) {
	var (
		rows pgx.Rows
		err  error
	)
	q := `SELECT id, name, description,
	             COALESCE(inputs, '[]'::jsonb),
	             COALESCE(transforms, '[]'::jsonb),
	             COALESCE(outputs, '[]'::jsonb),
	             COALESCE(schedule, ''), enabled, COALESCE(created_by, ''),
	             created_at, updated_at
	      FROM pipelines`
	if createdBy == "" {
		rows, err = s.pool.Query(ctx, q+` ORDER BY created_at DESC, id ASC`)
	} else {
		rows, err = s.pool.Query(ctx,
			q+` WHERE created_by = $1 ORDER BY created_at DESC, id ASC`, createdBy)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pipeline.Pipeline
	for rows.Next() {
		var (
			p             pipeline.Pipeline
			inputsRaw     []byte
			transformsRaw []byte
			outputsRaw    []byte
		)
		if err := rows.Scan(&p.ID, &p.Name, &p.Description,
			&inputsRaw, &transformsRaw, &outputsRaw,
			&p.Schedule, &p.Enabled, &p.CreatedBy,
			&p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(coalesceJSON(inputsRaw, "[]"), &p.Inputs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(coalesceJSON(transformsRaw, "[]"), &p.Transforms); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(coalesceJSON(outputsRaw, "[]"), &p.Outputs); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *pgPipelineStore) UpdatePipeline(ctx context.Context, id string, upd pipeline.PipelineUpdate) error {
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
	if upd.Inputs != nil {
		raw, err := json.Marshal(coalesceInputs(*upd.Inputs))
		if err != nil {
			return err
		}
		sets = append(sets, "inputs = $"+strconv.Itoa(argN))
		args = append(args, raw)
		argN++
	}
	if upd.Transforms != nil {
		raw, err := json.Marshal(coalesceTransforms(*upd.Transforms))
		if err != nil {
			return err
		}
		sets = append(sets, "transforms = $"+strconv.Itoa(argN))
		args = append(args, raw)
		argN++
	}
	if upd.Outputs != nil {
		raw, err := json.Marshal(coalesceOutputs(*upd.Outputs))
		if err != nil {
			return err
		}
		sets = append(sets, "outputs = $"+strconv.Itoa(argN))
		args = append(args, raw)
		argN++
	}
	if upd.Schedule != nil {
		sets = append(sets, "schedule = $"+strconv.Itoa(argN))
		args = append(args, *upd.Schedule)
		argN++
	}
	if upd.Enabled != nil {
		sets = append(sets, "enabled = $"+strconv.Itoa(argN))
		args = append(args, *upd.Enabled)
		argN++
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx,
		`UPDATE pipelines SET `+strings.Join(sets, ", ")+
			` WHERE id = $`+strconv.Itoa(argN), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrPipelineNotFound
	}
	return nil
}

func (s *pgPipelineStore) DeletePipeline(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM pipelines WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrPipelineNotFound
	}
	return nil
}

// AppendPipelineRun inserts one execution row and stamps run.ID +
// run.CreatedAt back onto the caller's pointer. Returns
// pipeline.ErrPipelineNotFound on FK violation so the caller can route
// to a 404.
func (s *pgPipelineStore) AppendPipelineRun(ctx context.Context, run *pipeline.PipelineRun) error {
	if run == nil {
		return errors.New("pipeline: run is nil")
	}
	resultJSON, err := json.Marshal(run.Result)
	if err != nil {
		return err
	}
	if len(resultJSON) == 0 || string(resultJSON) == "null" {
		resultJSON = []byte("{}")
	}
	startedAt := run.StartedAt
	if startedAt.IsZero() {
		startedAt = run.CreatedAt
	}
	err = s.pool.QueryRow(ctx,
		`INSERT INTO pipeline_runs
		   (pipeline_id, status, started_at, finished_at, error_message, run_result, triggered_by)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, created_at, started_at`,
		run.PipelineID, run.Status, startedAt, run.FinishedAt,
		run.ErrorMessage, resultJSON, run.TriggeredBy,
	).Scan(&run.ID, &run.CreatedAt, &run.StartedAt)
	if err != nil {
		if strings.Contains(err.Error(), "violates foreign key constraint") {
			return pipeline.ErrPipelineNotFound
		}
		return err
	}
	return nil
}

// GetPipelineRun fetches one run scoped to pipelineID.
// pipeline.ErrPipelineRunNotFound when missing OR when the row exists
// under a different pipeline (don't leak existence across pipelines).
func (s *pgPipelineStore) GetPipelineRun(ctx context.Context, pipelineID string, runID int64) (*pipeline.PipelineRun, error) {
	var (
		run         pipeline.PipelineRun
		resultRaw   []byte
		finishedAt  *time.Time
		startedAt   time.Time
		createdAt   time.Time
		errMessage  string
		triggeredBy string
		status      string
	)
	err := s.pool.QueryRow(ctx,
		`SELECT id, pipeline_id, COALESCE(status, ''), started_at, finished_at,
		        COALESCE(error_message, ''),
		        COALESCE(run_result, '{}'::jsonb),
		        COALESCE(triggered_by, ''), created_at
		 FROM pipeline_runs WHERE id = $1 AND pipeline_id = $2`, runID, pipelineID).
		Scan(&run.ID, &run.PipelineID, &status, &startedAt, &finishedAt,
			&errMessage, &resultRaw, &triggeredBy, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pipeline.ErrPipelineRunNotFound
		}
		return nil, err
	}
	run.Status = status
	run.StartedAt = startedAt
	run.FinishedAt = finishedAt
	run.ErrorMessage = errMessage
	run.TriggeredBy = triggeredBy
	run.CreatedAt = createdAt
	if len(resultRaw) > 0 && string(resultRaw) != "{}" {
		var rr pipeline.RunResult
		if err := json.Unmarshal(resultRaw, &rr); err == nil {
			run.Result = &rr
		}
	}
	return &run, nil
}

// ListPipelineRuns returns runs for pipelineID newest-first (descending
// id). Cursor is exclusive: when non-zero, only rows with id < cursor
// are returned. pipeline.ErrPipelineNotFound when the pipeline does not
// exist.
func (s *pgPipelineStore) ListPipelineRuns(ctx context.Context, pipelineID string, opts pipeline.ListRunsOptions) (*pipeline.ListRunsPage, error) {
	// Confirm the pipeline exists so an empty result is unambiguous to
	// the handler (404 vs 200 + []).
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pipelines WHERE id = $1)`, pipelineID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, pipeline.ErrPipelineNotFound
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	// Fetch limit+1 so we can compute hasMore without an extra COUNT.
	var (
		rows pgx.Rows
		err  error
	)
	q := `SELECT id, pipeline_id, COALESCE(status, ''), started_at, finished_at,
	             COALESCE(error_message, ''),
	             COALESCE(run_result, '{}'::jsonb),
	             COALESCE(triggered_by, ''), created_at
	      FROM pipeline_runs WHERE pipeline_id = $1`
	if opts.Cursor > 0 {
		rows, err = s.pool.Query(ctx,
			q+` AND id < $2 ORDER BY id DESC LIMIT $3`,
			pipelineID, opts.Cursor, limit+1)
	} else {
		rows, err = s.pool.Query(ctx,
			q+` ORDER BY id DESC LIMIT $2`,
			pipelineID, limit+1)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	page := &pipeline.ListRunsPage{Runs: make([]*pipeline.PipelineRun, 0, limit)}
	for rows.Next() {
		var (
			run        pipeline.PipelineRun
			resultRaw  []byte
			finishedAt *time.Time
		)
		if err := rows.Scan(&run.ID, &run.PipelineID, &run.Status, &run.StartedAt, &finishedAt,
			&run.ErrorMessage, &resultRaw, &run.TriggeredBy, &run.CreatedAt); err != nil {
			return nil, err
		}
		run.FinishedAt = finishedAt
		if len(resultRaw) > 0 && string(resultRaw) != "{}" {
			var rr pipeline.RunResult
			if err := json.Unmarshal(resultRaw, &rr); err == nil {
				run.Result = &rr
			}
		}
		page.Runs = append(page.Runs, &run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(page.Runs) > limit {
		page.Runs = page.Runs[:limit]
		page.NextCursor = page.Runs[len(page.Runs)-1].ID
	}
	return page, nil
}

func coalesceInputs(in []pipeline.Input) []pipeline.Input {
	if in == nil {
		return []pipeline.Input{}
	}
	return in
}

func coalesceTransforms(in []pipeline.Transform) []pipeline.Transform {
	if in == nil {
		return []pipeline.Transform{}
	}
	return in
}

func coalesceOutputs(in []pipeline.Output) []pipeline.Output {
	if in == nil {
		return []pipeline.Output{}
	}
	return in
}
