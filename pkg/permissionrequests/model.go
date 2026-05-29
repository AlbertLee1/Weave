// Package permissionrequests implements the share-link permission
// request workflow (US-339). When a user receives a share link to an
// object they cannot read, the SPA POSTs a permission_request row;
// approvers (admin / ontology-owner) review and transition the row to
// APPROVED or REJECTED. Notifications fan out via the existing OMS
// notifications table — to every approver on create, and back to the
// requester on decision.
package permissionrequests

import (
	"errors"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/rid"
)

// Status values are kept string-typed on the wire so SDKs can switch on
// them without importing a Go enum. Mirrors actions.ActionApproval.Status.
const (
	StatusPending  = "PENDING"
	StatusApproved = "APPROVED"
	StatusRejected = "REJECTED"
	// StatusCancelled (round 63) is the terminal state a row enters
	// when the requester withdraws their own pending request. Stored
	// alongside APPROVED / REJECTED so the audit trail keeps the row
	// rather than hard-deleting; the inbox UI hides canceled rows
	// by default.
	StatusCancelled = "CANCELLED"
)

// MaxReasonLength bounds both the requester's reason and the approver's
// decision note. 4 KiB is generous for free-form context without enabling
// pathological dumps and matches the migration CHECK constraint.
const MaxReasonLength = 4 * 1024

// DefaultPageLimit / MaxPageLimit bound List paging. The handler clamps
// caller-supplied limits to [1, MaxPageLimit].
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// Request is the wire + DB shape for a permission request row.
//
// DecidedAt is nil while Status is PENDING and non-nil once the row
// transitions to APPROVED or REJECTED. DecidedBy / DecisionNote stay
// empty strings until the decision is recorded.
type Request struct {
	ID           string     `json:"id"`
	TargetRID    string     `json:"targetRid"`
	RequestedBy  string     `json:"requestedBy"`
	Reason       string     `json:"reason,omitempty"`
	Status       string     `json:"status"`
	DecidedBy    string     `json:"decidedBy,omitempty"`
	DecisionNote string     `json:"decisionNote,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	DecidedAt    *time.Time `json:"decidedAt,omitempty"`
}

// Decision is the partial-update payload used by Approve / Reject. The
// store sets Status, DecidedBy, and DecidedAt atomically and copies the
// caller-supplied Note into DecisionNote.
type Decision struct {
	Status string
	By     string
	Note   string
}

// ValidateTargetRID enforces the canonical RID prefix on the resource a
// requester is asking access to. Centralised so handlers, store impls,
// and tests share the same rule.
func ValidateTargetRID(targetRID string) error {
	if strings.TrimSpace(targetRID) == "" {
		return errors.New("targetRid must not be empty")
	}
	if !rid.IsRID(targetRID) {
		return errors.New("targetRid must be a Resource Identifier (ri.<service>.<realm>.<type>.<id>)")
	}
	return nil
}

// ValidateReason caps the requester's free-form reason string. Empty is
// allowed — the requester does not have to justify themselves.
func ValidateReason(reason string) error {
	if len(reason) > MaxReasonLength {
		return errors.New("reason exceeds maximum length")
	}
	return nil
}

// IsTerminalStatus reports whether s is APPROVED, REJECTED or
// CANCELLED. Used by the store and handler to short-circuit double-
// decide / double-cancel attempts with ErrAlreadyDecided.
func IsTerminalStatus(s string) bool {
	return s == StatusApproved || s == StatusRejected || s == StatusCancelled
}
