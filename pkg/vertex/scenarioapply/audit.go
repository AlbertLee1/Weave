package scenarioapply

import (
	"context"
	"errors"
)

// ErrPermissionDenied is returned when the apply call cannot proceed
// because the user lacks permission on one of the underlying actions.
// Surfaced before any tx work so the rollback is conceptually free.
var ErrPermissionDenied = errors.New("permission denied")

// AuditKind enumerates the audit_log entry kinds Apply produces.
type AuditKind string

const (
	AuditKindActionExecuted  AuditKind = "action_executed"
	AuditKindScenarioApplied AuditKind = "scenario_applied"
)

// AuditEntry is the per-row shape an Apply call emits. Real impls
// persist these via pkg/audit.
type AuditEntry struct {
	Kind        AuditKind
	ScenarioRID string
	Op          string
}

// AuditLogger writes one entry at a time.
type AuditLogger interface {
	Log(ctx context.Context, e AuditEntry) error
}

// PermChecker validates the caller can execute a given action op (or
// any other gated operation). Real impl wires pkg/security checks.
type PermChecker interface {
	CheckActionPermission(ctx context.Context, op string) error
}

// AuditedService wraps Service with audit + permission gating. The
// original Service.Apply remains the lean apply-only flow used in
// systems / migrations; AuditedService.Apply is the user-facing entry.
type AuditedService struct {
	*Service
	audit AuditLogger
	perm  PermChecker
}

// NewServiceWithAudit composes the audited variant. Panics on any nil
// dep — all are required.
func NewServiceWithAudit(
	r ScenarioReader, w MainWriter, tx TxRunner,
	a AuditLogger, p PermChecker,
) *AuditedService {
	if a == nil || p == nil {
		panic("scenarioapply: nil audit/perm dep")
	}
	return &AuditedService{
		Service: NewService(r, w, tx),
		audit:   a,
		perm:    p,
	}
}

// Apply performs the audited flow:
//  1. Load scenario + edits.
//  2. Pre-flight: check permission on every edit op before any tx work.
//     A single denial aborts the entire apply with ErrPermissionDenied.
//  3. Inside the tx: ApplyEditToMain + audit action_executed per edit;
//     MarkScenarioApplied + audit scenario_applied; publish EditBatch.
//
// The pre-flight ensures that on permission failure NO edits hit main
// (no need to rely on tx rollback to fix mid-apply writes).
func (s *AuditedService) Apply(ctx context.Context, rid string) error {
	sc, err := s.reader.GetScenario(ctx, rid)
	if err != nil {
		return err
	}
	if sc.Status == ScenarioStatusApplied {
		return ErrAlreadyApplied
	}
	edits, err := s.reader.ListEdits(ctx, rid)
	if err != nil {
		return err
	}
	for _, e := range edits {
		if err := s.perm.CheckActionPermission(ctx, e.Op); err != nil {
			return err
		}
	}
	return s.tx.RunTx(ctx, func(ctx context.Context) error {
		for _, e := range edits {
			if err := s.writer.ApplyEditToMain(ctx, e); err != nil {
				return err
			}
			if err := s.audit.Log(ctx, AuditEntry{
				Kind:        AuditKindActionExecuted,
				ScenarioRID: rid,
				Op:          e.Op,
			}); err != nil {
				return err
			}
		}
		if err := s.writer.MarkScenarioApplied(ctx, rid); err != nil {
			return err
		}
		if err := s.audit.Log(ctx, AuditEntry{
			Kind:        AuditKindScenarioApplied,
			ScenarioRID: rid,
		}); err != nil {
			return err
		}
		return s.writer.PublishEditBatch(ctx, rid)
	})
}
