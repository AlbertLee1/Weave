package actions

import (
	"context"
	"encoding/json"
	"time"

	"github.com/liyang/weave/pkg/funnel"
)

// Saga lifecycle constants. The wire format mirrors the action_sagas
// CHECK constraint so SDK clients can switch on these strings without
// importing a Go enum.
const (
	SagaStatusRunning      = "RUNNING"
	SagaStatusSuccess      = "SUCCESS"
	SagaStatusCompensating = "COMPENSATING"
	SagaStatusCompensated  = "COMPENSATED"
	SagaStatusFailed       = "FAILED"
)

// Saga step lifecycle constants. PENDING is set at prepare-time;
// APPLIED indicates the primary edit batch committed; COMPENSATED
// means the inverse edit batch committed after rollback;
// COMPENSATION_FAILED means the inverse batch could not be built or
// published — those rows produce a row in action_saga_dlq.
const (
	SagaStepStatusPending             = "PENDING"
	SagaStepStatusApplied             = "APPLIED"
	SagaStepStatusFailed              = "FAILED"
	SagaStepStatusCompensated         = "COMPENSATED"
	SagaStepStatusCompensationFailed  = "COMPENSATION_FAILED"
)

// DLQ row status constants.
const (
	SagaDLQStatusPending  = "PENDING"
	SagaDLQStatusResolved = "RESOLVED"
	SagaDLQStatusDropped  = "DROPPED"
)

// Saga is the durable header row for a single applySaga invocation. The
// IdempotencyKey is the caller-supplied dedupe token: when a request
// repeats the same key the executor reads ResultJSON back instead of
// re-running the saga. Status follows the lifecycle constants above.
type Saga struct {
	SagaID          string          `json:"sagaId"`
	IdempotencyKey  string          `json:"idempotencyKey,omitempty"`
	Ontology        string          `json:"ontology"`
	Status          string          `json:"status"`
	RequestedBy     string          `json:"requestedBy,omitempty"`
	FailureMessage  string          `json:"failureMessage,omitempty"`
	ResultJSON      json.RawMessage `json:"-"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

// SagaStep is one declared step in a saga. EditsJSON snapshots the
// per-step edits actually built during prepare; InverseEditsJSON
// snapshots the compensator's edits when (and if) compensation runs.
// Both are nil until the saga reaches the corresponding phase.
type SagaStep struct {
	StepID            string          `json:"stepId"`
	SagaID            string          `json:"sagaId"`
	StepIndex         int             `json:"stepIndex"`
	ActionType        string          `json:"actionType"`
	Parameters        json.RawMessage `json:"parameters,omitempty"`
	EditsJSON         json.RawMessage `json:"editsJson,omitempty"`
	InverseEditsJSON  json.RawMessage `json:"inverseEditsJson,omitempty"`
	Status            string          `json:"status"`
	CreatedAt         time.Time       `json:"createdAt"`
	UpdatedAt         time.Time       `json:"updatedAt"`
}

// SagaDLQEntry is a dead-letter-queue row written when a compensator
// fails to prepare or commit. The retry handler reads PENDING rows and
// replays the inverse edit batch. Operators can DROP a row after
// investigation if replay is not appropriate.
type SagaDLQEntry struct {
	DLQID           string          `json:"dlqId"`
	SagaID          string          `json:"sagaId"`
	StepID          string          `json:"stepId"`
	Ontology        string          `json:"ontology"`
	EditsJSON       json.RawMessage `json:"editsJson,omitempty"`
	FailureMessage  string          `json:"failureMessage,omitempty"`
	Status          string          `json:"status"`
	Attempts        int             `json:"attempts"`
	LastAttemptAt   *time.Time      `json:"lastAttemptAt,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

// SagaStore is the narrow persistence surface used by ApplyBatchSaga to
// record saga lifecycle, per-step state, and the DLQ. Kept off
// oms.Repository so the rest of the test tree's mock repos do not need
// new stubs — concrete PG implementation lives in cmd/server/. Same
// shape as ActionApprovalStore / ActionJobStore.
type SagaStore interface {
	// CreateSaga inserts the header row in RUNNING status. Returning a
	// non-nil error with errors.Is(err, ErrSagaIdempotencyConflict) means
	// the idempotency_key already exists; callers should follow up with
	// GetSagaByIdempotencyKey to read the previous result.
	CreateSaga(ctx context.Context, s *Saga) error
	// GetSagaByIdempotencyKey returns the saga row whose idempotency_key
	// matches, or oms.ErrNotFound when no such row exists. The empty
	// string is treated as "no key" and always returns ErrNotFound.
	GetSagaByIdempotencyKey(ctx context.Context, key string) (*Saga, error)
	// GetSaga returns a saga by saga_id.
	GetSaga(ctx context.Context, sagaID string) (*Saga, error)
	// UpdateSagaStatus sets status + failure_message + result_json and
	// stamps updated_at. Pointer fields use the "nil = preserve"
	// convention so the executor can advance status without overwriting
	// a previously-stored result.
	UpdateSagaStatus(ctx context.Context, sagaID string, upd SagaUpdate) error

	// CreateSagaStep inserts a per-step row in PENDING status.
	CreateSagaStep(ctx context.Context, step *SagaStep) error
	// UpdateSagaStep advances the step's status and (optionally) sets
	// edits_json / inverse_edits_json.
	UpdateSagaStep(ctx context.Context, stepID string, upd SagaStepUpdate) error
	// ListSagaSteps returns all steps for a saga ordered by step_index.
	ListSagaSteps(ctx context.Context, sagaID string) ([]*SagaStep, error)

	// EnqueueDLQ records a compensator failure for manual / scheduled
	// retry.
	EnqueueDLQ(ctx context.Context, entry *SagaDLQEntry) error
	// ListDLQ returns DLQ rows filtered by status. Empty status returns
	// every row.
	ListDLQ(ctx context.Context, status string, limit int) ([]*SagaDLQEntry, error)
	// UpdateDLQStatus transitions a DLQ row (e.g. PENDING → RESOLVED
	// after a successful retry, or PENDING → DROPPED after manual
	// dismissal).
	UpdateDLQStatus(ctx context.Context, dlqID string, upd SagaDLQUpdate) error
}

// SagaUpdate is the partial-update payload for action_sagas. Pointer
// fields use the "nil = preserve" convention.
type SagaUpdate struct {
	Status         string
	FailureMessage *string
	ResultJSON     json.RawMessage
}

// SagaStepUpdate is the partial-update payload for action_saga_steps.
type SagaStepUpdate struct {
	Status            string
	EditsJSON         json.RawMessage
	InverseEditsJSON  json.RawMessage
}

// SagaDLQUpdate is the partial-update payload for action_saga_dlq.
type SagaDLQUpdate struct {
	Status         string
	Attempts       *int
	FailureMessage *string
	LastAttemptAt  *time.Time
}

// ErrSagaIdempotencyConflict is the sentinel returned by CreateSaga
// when the idempotency_key already exists. Callers detect this via
// errors.Is and replay the prior result instead of running again.
var ErrSagaIdempotencyConflict = sagaIdempotencyConflictErr{}

type sagaIdempotencyConflictErr struct{}

func (sagaIdempotencyConflictErr) Error() string {
	return "saga idempotency_key conflict"
}

// MarshalEdits is a small helper used by both the executor and the PG
// store to canonicalise an edit slice into JSON without re-implementing
// the pattern at every call site. Returns "[]" for empty or nil input
// so a NOT NULL DEFAULT '[]'::jsonb column round-trips cleanly.
func MarshalEdits(edits []funnel.Edit) (json.RawMessage, error) {
	if len(edits) == 0 {
		return json.RawMessage("[]"), nil
	}
	b, err := json.Marshal(edits)
	if err != nil {
		return nil, err
	}
	return b, nil
}
