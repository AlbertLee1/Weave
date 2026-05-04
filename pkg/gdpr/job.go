// Package gdpr implements the right-to-be-forgotten flow for the
// /api/admin/gdpr/erase admin endpoint (US-267).
//
// The erase flow is async: the handler creates a PENDING ErasureJob,
// returns 202 Accepted with {jobId, status}, then a detached goroutine
// walks a fixed list of Steps (revoke sessions, delete user, redact
// audit, ...) and updates the job row as it goes. Pollers fetch
// progress via GET /api/admin/gdpr/erase/{jobId}.
//
// "Audit records cannot be deleted (but redacted)" is satisfied by
// pkg/audit's RedactingStore decorator: every successful erase appends
// the user's actor_id to gdpr_redactions and the audit List path then
// scrubs PII for matching events while preserving the hash chain.
package gdpr

import (
	"context"
	"time"
)

// ErasureJob is the persisted state of one right-to-be-forgotten run.
// Mirrors actions.ActionJob (US-240) so SDK pollers see an identical
// envelope.
//
// ProofHash (US-443) is a deterministic sha256 hex digest stamped on the
// job at terminal time. It commits to the canonicalised tuple
// (userId, status, ordered step outcomes, errorMessage, requestedBy) so
// auditors can later prove that a specific erase run produced a specific
// outcome without re-fetching the row. Empty until the job reaches a
// terminal state (SUCCEEDED / FAILED).
type ErasureJob struct {
	JobID        string       `json:"jobId"`
	UserID       string       `json:"userId"`
	Status       string       `json:"status"`
	Progress     int          `json:"progress"`
	Steps        []StepResult `json:"steps,omitempty"`
	ErrorMessage string       `json:"errorMessage,omitempty"`
	RequestedBy  string       `json:"requestedBy,omitempty"`
	ProofHash    string       `json:"proofHash,omitempty"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
}

// Status constants. String-typed on the wire so SDKs switch without
// importing a Go enum. Mirrors actions.ActionJobStatus*.
const (
	JobStatusPending   = "PENDING"
	JobStatusRunning   = "RUNNING"
	JobStatusSucceeded = "SUCCEEDED"
	JobStatusFailed    = "FAILED"
)

// StepResult records the outcome of a single erase step. RowsAffected
// is the count of records the step removed / updated; useful for
// audit-log reconstruction. ErrorMessage is empty on success.
type StepResult struct {
	Name         string `json:"name"`
	RowsAffected int    `json:"rowsAffected"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	DurationMs   int64  `json:"durationMs"`
}

// JobUpdate is the partial-update payload used by JobStore.Update. nil /
// empty fields are preserved (three-state shape, mirrors
// actions.ActionJobUpdate). Steps replaces the entire list when set —
// callers are expected to append the new step then send the full slice
// because SQL JSONB doesn't support partial-array updates portably.
//
// ProofHash is the terminal proof-of-erasure digest (US-443). Only the
// terminal write sets it; intermediate progress writes leave it nil so
// the column stays empty until SUCCEEDED / FAILED.
type JobUpdate struct {
	Status       string
	Progress     *int
	Steps        []StepResult
	ErrorMessage *string
	ProofHash    *string
}

// JobStore is the narrow CRUD surface the orchestrator + handler depend
// on. Same pattern as actions.ActionJobStore — kept off oms.Repository
// so the dozens of mock repos don't need new stubs.
type JobStore interface {
	CreateJob(ctx context.Context, job *ErasureJob) error
	GetJob(ctx context.Context, id string) (*ErasureJob, error)
	UpdateJob(ctx context.Context, id string, upd JobUpdate) error
}

// EraseRequest is the wire shape for POST /api/admin/gdpr/erase.
type EraseRequest struct {
	UserID string `json:"userId"`
}

// EraseResponse is the 202 Accepted envelope.
type EraseResponse struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}
