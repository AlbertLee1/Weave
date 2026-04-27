package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

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
