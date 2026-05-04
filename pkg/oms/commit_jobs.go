package oms

import (
	"context"
	"errors"
	"time"
)

// US-417: per-commit CI job for a Function code-repo revision.
//
// CommitJob captures the outcome of the lint + test pipeline that runs in
// the background after a successful POST /commits on a Function's bare git
// repo. The badge in the Function diff UI reads the `Status` field; the
// per-phase output strings let the UI surface a tooltip / drawer with the
// raw stdout/stderr for diagnosis without re-running the job.

// CommitJobStatus enumerates the lifecycle states a CI job can be in.
type CommitJobStatus string

const (
	// CommitJobStatusQueued is the initial state when the row is inserted.
	CommitJobStatusQueued CommitJobStatus = "queued"
	// CommitJobStatusRunning is set when the runner picks up the row and
	// begins executing the lint / test phases.
	CommitJobStatusRunning CommitJobStatus = "running"
	// CommitJobStatusSuccess is set when both phases completed without
	// errors.
	CommitJobStatusSuccess CommitJobStatus = "success"
	// CommitJobStatusFailure is set when either phase returned a non-zero
	// status / surfaced a parse or test error.
	CommitJobStatusFailure CommitJobStatus = "failure"
	// CommitJobStatusSkipped is set when the runner could not perform the
	// pipeline (no tools available, no test code present, …) but the
	// commit itself is fine.
	CommitJobStatusSkipped CommitJobStatus = "skipped"
)

// CommitJob is the persisted row for one CI run.
type CommitJob struct {
	ID           int64           `json:"id"`
	FunctionRID  string          `json:"functionRid"`
	CommitSha    string          `json:"commitSha"`
	Status       CommitJobStatus `json:"status"`
	LintOutput   string          `json:"lintOutput,omitempty"`
	TestOutput   string          `json:"testOutput,omitempty"`
	ErrorMessage string          `json:"errorMessage,omitempty"`
	StartedAt    *time.Time      `json:"startedAt,omitempty"`
	FinishedAt   *time.Time      `json:"finishedAt,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
	UpdatedAt    time.Time       `json:"updatedAt"`
}

// ErrCommitJobNotFound is returned by CommitJobStore methods when no row
// matches the supplied (functionRid, commitSha) pair.
var ErrCommitJobNotFound = errors.New("oms: commit job not found")

// CommitJobStore is the narrow durable surface for the per-commit CI job
// records. The store is OPTIONAL: degraded-mode bootstraps (no PG) leave it
// nil and the Function repo commit handler skips the job-recording step.
//
// Defined here (not on Repository) for the same reason as
// FunctionRepoStore — degraded-mode test routers should not have to
// cascade-stub it on top of every Repository mock.
type CommitJobStore interface {
	// UpsertCommitJob inserts a new row keyed by (function_rid, commit_sha)
	// or updates the existing row in place. The ID + CreatedAt fields are
	// back-filled on the supplied pointer; UpdatedAt is always refreshed.
	UpsertCommitJob(ctx context.Context, job *CommitJob) error
	// GetCommitJob looks up the job by (function_rid, commit_sha). Returns
	// ErrCommitJobNotFound when no such row exists.
	GetCommitJob(ctx context.Context, functionRID, commitSha string) (*CommitJob, error)
	// ListCommitJobs returns every job for the supplied Function, newest-
	// first, capped at `limit` (limit <= 0 returns the entire history).
	ListCommitJobs(ctx context.Context, functionRID string, limit int) ([]CommitJob, error)
}

// CommitJobRunInput is the payload supplied to a CommitJobRunner. The
// runner gets just the bytes it needs to run lint + test against the new
// revision; the wider Function/Ontology context lives on the store row
// for any future enrichment.
type CommitJobRunInput struct {
	FunctionRID string
	CommitSha   string
	SourceCode  string
}

// CommitJobRunResult is the outcome the runner records back into the
// store. The status is one of CommitJobStatusSuccess / Failure / Skipped;
// LintOutput / TestOutput carry the raw per-phase output (truncated to a
// reasonable size by the runner so a runaway log can't blow up PG row
// limits); ErrorMessage carries a one-line summary for the UI badge
// tooltip when status is Failure.
type CommitJobRunResult struct {
	Status       CommitJobStatus
	LintOutput   string
	TestOutput   string
	ErrorMessage string
}

// CommitJobRunner is the pluggable execution surface that runs the
// lint + test phases against a freshly-committed revision. The default
// implementation in cmd/server/ uses a Goja-based syntactic check for
// lint and an embedded test-runner for `// @test` blocks; production
// deploys can swap in an eslint/vitest shell-out without touching the
// surrounding handler logic.
//
// Implementations MUST be safe for concurrent invocation — the handler
// fires one goroutine per commit and a Function can take several
// commits in flight at once.
type CommitJobRunner interface {
	RunCommitJob(ctx context.Context, in CommitJobRunInput) CommitJobRunResult
}
