package main

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/pkg/actions"
	"github.com/liyang/weave/pkg/oms"
)

// pgActionJobStore satisfies actions.ActionJobStore by persisting job state
// to the action_jobs table (US-240). Lives in cmd/server/ rather than
// pkg/oms/ so the dependency direction stays clean — pkg/oms cannot import
// pkg/actions (actions already imports oms). Same shape as
// pgEdgePropertiesResolver / interface_method_dispatcher.
type pgActionJobStore struct {
	pool *pgxpool.Pool
}

func newPGActionJobStore(pool *pgxpool.Pool) *pgActionJobStore {
	return &pgActionJobStore{pool: pool}
}

func (s *pgActionJobStore) CreateActionJob(ctx context.Context, job *actions.ActionJob) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO action_jobs
		   (job_id, ontology_api_name, action_type, status, progress, result, error_message, created_by)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''::jsonb), $7, $8)`,
		job.JobID, job.OntologyAPI, job.ActionTypeName,
		job.Status, job.Progress,
		coerceJSON(job.Result), job.ErrorMessage, job.CreatedBy,
	)
	return err
}

func (s *pgActionJobStore) GetActionJob(ctx context.Context, id string) (*actions.ActionJob, error) {
	var job actions.ActionJob
	var result []byte
	err := s.pool.QueryRow(ctx,
		`SELECT job_id, ontology_api_name, action_type, status, progress,
		        COALESCE(result, '{}'::jsonb), error_message, created_by, created_at, updated_at
		 FROM action_jobs WHERE job_id = $1`, id).
		Scan(&job.JobID, &job.OntologyAPI, &job.ActionTypeName,
			&job.Status, &job.Progress, &result,
			&job.ErrorMessage, &job.CreatedBy, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oms.ErrNotFound
		}
		return nil, err
	}
	// Treat stored "{}" as empty so the wire omits the field via omitempty.
	if len(result) == 0 || string(result) == "{}" {
		job.Result = nil
	} else {
		job.Result = result
	}
	return &job, nil
}

func (s *pgActionJobStore) UpdateActionJob(ctx context.Context, id string, upd actions.ActionJobUpdate) error {
	// Build a dynamic UPDATE touching only the fields the caller set. Keeps
	// the PATCH shape honest — callers that send Progress alone won't clobber
	// Status, and vice versa. Mirrors the partial-update convention from
	// UpdateLinkType / UpdateFunction handlers.
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
	if upd.Result != nil {
		sets = append(sets, "result = NULLIF($"+strconv.Itoa(argN)+", ''::jsonb)")
		args = append(args, coerceJSON(upd.Result))
		argN++
	}
	if upd.ErrorMessage != nil {
		sets = append(sets, "error_message = $"+strconv.Itoa(argN))
		args = append(args, *upd.ErrorMessage)
		argN++
	}
	args = append(args, id)
	tag, err := s.pool.Exec(ctx,
		`UPDATE action_jobs SET `+strings.Join(sets, ", ")+` WHERE job_id = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return oms.ErrNotFound
	}
	return nil
}

// coerceJSON substitutes "{}" for nil json.RawMessage. pgx encodes a raw nil
// as the literal string "null" which the JSONB column accepts but breaks the
// "empty ⇒ omitted" round-trip. Same reason pkg/oms/function.go
// normaliseSignatureForWrite exists — see progress.txt.
func coerceJSON(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

