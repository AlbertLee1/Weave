package actions

import (
	"context"
	"encoding/json"
	"time"
)

// ActionJob is the persisted state of an async action apply. US-240.
//
// Status follows a strict transition: PENDING → RUNNING → (SUCCEEDED | FAILED).
// Progress is 0..100; terminal states are always 100 (SUCCEEDED) or the last
// reported value (FAILED). Result holds the sync-apply response payload when
// the job succeeds (marshalled SyncApplyActionResponseV2) so pollers can pull
// the same envelope they would have received from the sync path. ErrorMessage
// is populated on FAILED.
type ActionJob struct {
	JobID          string          `json:"jobId"`
	OntologyAPI    string          `json:"ontologyApiName"`
	ActionTypeName string          `json:"actionType"`
	Status         string          `json:"status"`
	Progress       int             `json:"progress"`
	Result         json.RawMessage `json:"result,omitempty"`
	ErrorMessage   string          `json:"errorMessage,omitempty"`
	CreatedBy      string          `json:"createdBy,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

// ActionJob status constants. Kept string-typed on the wire so SDKs can
// switch on them without importing a Go enum.
const (
	ActionJobStatusPending   = "PENDING"
	ActionJobStatusRunning   = "RUNNING"
	ActionJobStatusSucceeded = "SUCCEEDED"
	ActionJobStatusFailed    = "FAILED"
)

// ActionJobUpdate is the partial-update payload for UpdateActionJob. Fields
// that are nil / empty are preserved (three-state pattern — mirrors
// UpdateLinkTypeRequest.IsRequired *bool, see progress.txt). Status="" is the
// "don't change" marker; explicit PENDING/RUNNING/... transitions are the
// intended values.
type ActionJobUpdate struct {
	Status       string
	Progress     *int
	Result       json.RawMessage
	ErrorMessage *string
}

// ActionJobStore is the narrow CRUD surface the Executor + handler depend on.
// Following the MediaAssetStore / ComputedPropertyStore convention — kept OFF
// oms.Repository so mock repos scattered across the test tree do not need new
// stubs. Concrete PG implementation lives in pkg/oms/pg_repository_action_job.go.
type ActionJobStore interface {
	CreateActionJob(ctx context.Context, job *ActionJob) error
	GetActionJob(ctx context.Context, id string) (*ActionJob, error)
	UpdateActionJob(ctx context.Context, id string, upd ActionJobUpdate) error
}

// AsyncApplyResponse is the 202 Accepted envelope returned by the async
// apply path. Includes the initial status so lightweight pollers can skip
// the first GET if they see SUCCEEDED already.
type AsyncApplyResponse struct {
	JobID  string `json:"jobId"`
	Status string `json:"status"`
}

// ActionJobResponse is the wire shape returned by GET .../actions/jobs/{id}.
// Intentionally identical to ActionJob today — kept as a separate alias so
// future fields (timestamps, host metadata) can be added to the stored row
// without breaking the wire contract.
type ActionJobResponse = ActionJob
