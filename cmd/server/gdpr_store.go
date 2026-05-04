package main

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/gdpr"
)

// jsonMarshalSteps encodes a StepResult slice for the steps JSONB
// column. Empty slice serialises to "[]" so the column never holds a
// SQL NULL after a successful write.
func jsonMarshalSteps(steps []gdpr.StepResult) ([]byte, error) {
	if steps == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(steps)
}

// jsonUnmarshalSteps reads the steps JSONB column. Returns an empty
// slice (not nil) on empty input so handlers can range over the result
// without nil checks.
func jsonUnmarshalSteps(data []byte) ([]gdpr.StepResult, error) {
	if len(data) == 0 {
		return []gdpr.StepResult{}, nil
	}
	var out []gdpr.StepResult
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []gdpr.StepResult{}
	}
	return out, nil
}

// pgGDPRJobStore satisfies gdpr.JobStore by persisting erase-job state
// to the gdpr_erasure_jobs table (US-267). Lives in cmd/server/ rather
// than pkg/gdpr/ so the dependency direction stays clean — pkg/gdpr
// stays free of any pgx import. Same shape as pgActionJobStore.
type pgGDPRJobStore struct {
	pool *pgxpool.Pool
}

func newPGGDPRJobStore(pool *pgxpool.Pool) *pgGDPRJobStore {
	return &pgGDPRJobStore{pool: pool}
}

func (s *pgGDPRJobStore) CreateJob(ctx context.Context, job *gdpr.ErasureJob) error {
	steps := job.Steps
	if steps == nil {
		steps = []gdpr.StepResult{}
	}
	stepsJSON, err := jsonMarshalSteps(steps)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx,
		`INSERT INTO gdpr_erasure_jobs
		   (job_id, user_id, status, progress, steps, error_message, requested_by, proof_hash)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		job.JobID, job.UserID, job.Status, job.Progress,
		stepsJSON, job.ErrorMessage, job.RequestedBy, job.ProofHash,
	)
	return err
}

func (s *pgGDPRJobStore) GetJob(ctx context.Context, id string) (*gdpr.ErasureJob, error) {
	var job gdpr.ErasureJob
	var stepsJSON []byte
	err := s.pool.QueryRow(ctx,
		`SELECT job_id, user_id, status, progress,
		        COALESCE(steps, '[]'::jsonb), error_message, requested_by,
		        COALESCE(proof_hash, ''),
		        created_at, updated_at
		 FROM gdpr_erasure_jobs WHERE job_id = $1`, id).
		Scan(&job.JobID, &job.UserID, &job.Status, &job.Progress,
			&stepsJSON, &job.ErrorMessage, &job.RequestedBy,
			&job.ProofHash,
			&job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, gdpr.ErrJobNotFound
		}
		return nil, err
	}
	steps, err := jsonUnmarshalSteps(stepsJSON)
	if err != nil {
		return nil, err
	}
	job.Steps = steps
	return &job, nil
}

func (s *pgGDPRJobStore) UpdateJob(ctx context.Context, id string, upd gdpr.JobUpdate) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.Status != "" {
		sets = append(sets, "status = $"+strconv.Itoa(argN))
		args = append(args, upd.Status)
		argN++
	}
	if upd.Progress != nil {
		sets = append(sets, "progress = $"+strconv.Itoa(argN))
		args = append(args, *upd.Progress)
		argN++
	}
	if upd.Steps != nil {
		stepsJSON, err := jsonMarshalSteps(upd.Steps)
		if err != nil {
			return err
		}
		sets = append(sets, "steps = $"+strconv.Itoa(argN))
		args = append(args, stepsJSON)
		argN++
	}
	if upd.ErrorMessage != nil {
		sets = append(sets, "error_message = $"+strconv.Itoa(argN))
		args = append(args, *upd.ErrorMessage)
		argN++
	}
	if upd.ProofHash != nil {
		sets = append(sets, "proof_hash = $"+strconv.Itoa(argN))
		args = append(args, *upd.ProofHash)
		argN++
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx,
		`UPDATE gdpr_erasure_jobs SET `+strings.Join(sets, ", ")+
			` WHERE job_id = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return gdpr.ErrJobNotFound
	}
	return nil
}
