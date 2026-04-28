// Package comments implements per-RID threaded discussion persistence
// (US-334). One row captures a comment body authored against a target
// RID (object, action log, or any future commentable resource), with an
// optional parent_id to model reply threads. Soft-deleted rows survive
// in place so the audit trail and reply chains stay intact.
package comments

import (
	"errors"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/rid"
)

// Comment is the wire + DB shape for a persisted comment.
//
// DeletedAt is nil for live rows. Once non-nil, Body is overwritten with
// the empty string at the store layer so soft-deleted content does not
// leak through List/Get; the row stays so reply chains keep their
// parent reference.
type Comment struct {
	ID        string     `json:"id"`
	TargetRID string     `json:"targetRid"`
	Body      string     `json:"body"`
	Author    string     `json:"author"`
	ParentID  string     `json:"parentId,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	DeletedAt *time.Time `json:"deletedAt,omitempty"`
}

// Update is the partial-update DTO. Only Body is mutable post-create —
// authorship, target, and parent are immutable invariants. nil pointer
// preserves the existing value; non-nil overwrites.
type Update struct {
	Body *string `json:"body,omitempty"`
}

// MaxBodyLength bounds comment text on both create and update so the
// CHECK constraint and the Go-side validator agree. 8 KiB is enough for
// long-form review comments without enabling pathological dumps.
const MaxBodyLength = 8 * 1024

// DefaultPageLimit / MaxPageLimit bound List paging. The handler clamps
// caller-supplied limits to [1, MaxPageLimit].
const (
	DefaultPageLimit = 50
	MaxPageLimit     = 200
)

// ValidateBody ensures a comment body is non-empty after trim and no
// longer than MaxBodyLength. Centralised so handlers, store impls, and
// tests share the same rule.
func ValidateBody(body string) error {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return errors.New("body must not be empty")
	}
	if len(body) > MaxBodyLength {
		return errors.New("body exceeds maximum length")
	}
	return nil
}

// ValidateTargetRID enforces the canonical RID prefix on the target so
// the comments table never accumulates rows pointing at malformed
// identifiers.
func ValidateTargetRID(targetRID string) error {
	if strings.TrimSpace(targetRID) == "" {
		return errors.New("targetRid must not be empty")
	}
	if !rid.IsRID(targetRID) {
		return errors.New("targetRid must be a Resource Identifier (ri.<service>.<realm>.<type>.<id>)")
	}
	return nil
}
