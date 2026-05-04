package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liyang/weave/pkg/oms"
)

// pgCommitJobStore persists commit_jobs rows for the Function CI hook
// (US-417). Lives in cmd/server/ so pkg/oms doesn't import pgx — same
// shape as pgInstalledPackageStore.
type pgCommitJobStore struct {
	pool *pgxpool.Pool
}

func newPGCommitJobStore(pool *pgxpool.Pool) *pgCommitJobStore {
	return &pgCommitJobStore{pool: pool}
}

func (s *pgCommitJobStore) UpsertCommitJob(ctx context.Context, job *oms.CommitJob) error {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO commit_jobs
		   (function_rid, commit_sha, status, lint_output, test_output,
		    error_message, started_at, finished_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (function_rid, commit_sha) DO UPDATE SET
		   status        = EXCLUDED.status,
		   lint_output   = EXCLUDED.lint_output,
		   test_output   = EXCLUDED.test_output,
		   error_message = EXCLUDED.error_message,
		   started_at    = EXCLUDED.started_at,
		   finished_at   = EXCLUDED.finished_at,
		   updated_at    = NOW()
		 RETURNING id, created_at, updated_at`,
		job.FunctionRID, job.CommitSha, string(job.Status),
		job.LintOutput, job.TestOutput, job.ErrorMessage,
		job.StartedAt, job.FinishedAt,
	)
	return row.Scan(&job.ID, &job.CreatedAt, &job.UpdatedAt)
}

func (s *pgCommitJobStore) GetCommitJob(ctx context.Context, functionRID, commitSha string) (*oms.CommitJob, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, function_rid, commit_sha, status,
		        lint_output, test_output, error_message,
		        started_at, finished_at, created_at, updated_at
		   FROM commit_jobs
		  WHERE function_rid = $1 AND commit_sha = $2`,
		functionRID, commitSha)
	job := &oms.CommitJob{}
	var status string
	if err := row.Scan(
		&job.ID, &job.FunctionRID, &job.CommitSha, &status,
		&job.LintOutput, &job.TestOutput, &job.ErrorMessage,
		&job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oms.ErrCommitJobNotFound
		}
		return nil, err
	}
	job.Status = oms.CommitJobStatus(status)
	return job, nil
}

func (s *pgCommitJobStore) ListCommitJobs(ctx context.Context, functionRID string, limit int) ([]oms.CommitJob, error) {
	args := []interface{}{functionRID}
	query := `SELECT id, function_rid, commit_sha, status,
	                lint_output, test_output, error_message,
	                started_at, finished_at, created_at, updated_at
	            FROM commit_jobs
	           WHERE function_rid = $1
	           ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]oms.CommitJob, 0)
	for rows.Next() {
		var job oms.CommitJob
		var status string
		if err := rows.Scan(
			&job.ID, &job.FunctionRID, &job.CommitSha, &status,
			&job.LintOutput, &job.TestOutput, &job.ErrorMessage,
			&job.StartedAt, &job.FinishedAt, &job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, err
		}
		job.Status = oms.CommitJobStatus(status)
		out = append(out, job)
	}
	return out, rows.Err()
}
