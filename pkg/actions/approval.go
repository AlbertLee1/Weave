package actions

import (
	"context"
	"encoding/json"
	"time"
)

// ActionApproval is a persisted approval request produced when an apply call
// targets an ActionType flagged RequiresApproval. Status transitions linearly
// from PENDING to one of (APPROVED, REJECTED); the terminal states are final
// (409 Conflict on any re-approve attempt). Parameters is a snapshot of the
// caller's apply body so the approver can review what will execute if the
// request is approved. Approvers snapshots the ActionType's approver list at
// submission time — later edits to ActionType.Approvers don't retroactively
// change who can review an already-queued approval.
type ActionApproval struct {
	ID              string          `json:"id"`
	ActionTypeRID   string          `json:"actionTypeRid,omitempty"`
	OntologyAPIName string          `json:"ontologyApiName"`
	ActionType      string          `json:"actionType"`
	Parameters      json.RawMessage `json:"parameters,omitempty"`
	Approvers       []string        `json:"approvers,omitempty"`
	Status          string          `json:"status"`
	RequestedBy     string          `json:"requestedBy,omitempty"`
	ReviewedBy      string          `json:"reviewedBy,omitempty"`
	Reason          string          `json:"reason,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

// ActionApproval status constants — kept string-typed on the wire so SDKs can
// switch on them without importing a Go enum. Mirrors ActionJob status.
const (
	ActionApprovalStatusPending  = "PENDING"
	ActionApprovalStatusApproved = "APPROVED"
	ActionApprovalStatusRejected = "REJECTED"
)

// ActionApprovalUpdate is the partial-update payload. Pointer fields use the
// "nil = preserve" convention (see ActionJobUpdate). Status="" is the "don't
// change" marker; the handler always sets it to APPROVED or REJECTED.
type ActionApprovalUpdate struct {
	Status     string
	ReviewedBy *string
	Reason     *string
}

// ActionApprovalStore is the narrow persistence surface used by the approval
// workflow handlers. Kept OFF oms.Repository so the cascade of mock repos
// across the test tree (pkg/oms, pkg/oss, pkg/links, pkg/actions, etc.) does
// not need new stubs. Concrete PG implementation lives in
// cmd/server/action_approval_store.go. Mirrors ActionJobStore / MediaAssetStore
// conventions.
type ActionApprovalStore interface {
	CreateActionApproval(ctx context.Context, a *ActionApproval) error
	GetActionApproval(ctx context.Context, id string) (*ActionApproval, error)
	UpdateActionApproval(ctx context.Context, id string, upd ActionApprovalUpdate) error
}

// PendingApprovalResponse is the 202-Accepted envelope returned by Apply when
// the target ActionType requires approval. Carries the approval id so the
// caller can poll or navigate to the approval UI.
type PendingApprovalResponse struct {
	ApprovalID string `json:"approvalId"`
	Status     string `json:"status"`
}
