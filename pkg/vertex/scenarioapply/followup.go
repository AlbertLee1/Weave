package scenarioapply

import "context"

// FollowUpAction is a server-side Action to invoke AFTER an Apply has
// committed. The apply request body's followUpActions: [...] array is
// turned into a slice of these. Failure of a follow-up does NOT roll
// back the apply (which is already on disk); it is recorded as a
// followup_failed audit row instead, and the chain stops at the first
// failure.
type FollowUpAction struct {
	ActionTypeRID string
	Params        map[string]any
}

// ActionRunner is the contract for executing a single follow-up. Real
// impl wires pkg/actions.Executor.
type ActionRunner interface {
	ExecuteAction(ctx context.Context, a FollowUpAction) error
}

// Additional audit kind for follow-ups.
const (
	AuditKindFollowUpExecuted AuditKind = "followup_executed"
	AuditKindFollowUpFailed   AuditKind = "followup_failed"
)

// WithActionRunner attaches an ActionRunner so ApplyWithFollowUps can
// dispatch follow-ups. Calling WithActionRunner returns a new
// AuditedService; the original is left untouched.
func (s *AuditedService) WithActionRunner(runner ActionRunner) *AuditedService {
	if runner == nil {
		panic("scenarioapply: nil ActionRunner")
	}
	cp := *s
	cp.runner = runner
	return &cp
}

// runner is stored on AuditedService via the field below (declared here
// so the new build keeps audit.go untouched of follow-up concerns).
// We use a method-set extension through an unexported field.
//
// Implementation detail: since Go doesn't support "optional fields"
// cleanly via interface embedding, runner is a plain field nil-checked
// at call time.
//
// (Field declared at the AuditedService level — see audit.go.)

// ApplyWithFollowUps runs the audited apply, then if and only if the
// apply succeeds, dispatches every follow-up in order. The first
// failure stops the chain; the apply itself is already committed.
func (s *AuditedService) ApplyWithFollowUps(
	ctx context.Context,
	rid string,
	followUps []FollowUpAction,
) error {
	if err := s.Apply(ctx, rid); err != nil {
		return err
	}
	if s.runner == nil || len(followUps) == 0 {
		return nil
	}
	for _, fa := range followUps {
		if err := s.runner.ExecuteAction(ctx, fa); err != nil {
			_ = s.audit.Log(ctx, AuditEntry{
				Kind:        AuditKindFollowUpFailed,
				ScenarioRID: rid,
				Op:          fa.ActionTypeRID,
			})
			return nil // apply succeeded; follow-up failure is audited, not propagated
		}
		_ = s.audit.Log(ctx, AuditEntry{
			Kind:        AuditKindFollowUpExecuted,
			ScenarioRID: rid,
			Op:          fa.ActionTypeRID,
		})
	}
	return nil
}
