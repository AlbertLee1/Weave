package scenarioapply

import (
	"context"
	"errors"
	"testing"
)

type stubAudit struct {
	entries []AuditEntry
}

func (s *stubAudit) Log(_ context.Context, e AuditEntry) error {
	s.entries = append(s.entries, e)
	return nil
}

type stubPermChecker struct {
	deniedOp string
}

func (s *stubPermChecker) CheckActionPermission(_ context.Context, op string) error {
	if op == s.deniedOp {
		return ErrPermissionDenied
	}
	return nil
}

func TestApply_Given_3Edits_When_Apply_Then_AuditHas4Rows(t *testing.T) {
	r := &stubReader{
		scenarios: map[string]Scenario{
			"s1": {RID: "s1", Status: ScenarioStatusDraft},
		},
		edits: map[string][]ScenarioEdit{
			"s1": {{Op: "a"}, {Op: "b"}, {Op: "c"}},
		},
	}
	w := &stubWriter{}
	a := &stubAudit{}
	p := &stubPermChecker{}
	svc := NewServiceWithAudit(r, w, &stubTxRunner{}, a, p)

	if err := svc.Apply(context.Background(), "s1"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(a.entries) != 4 {
		t.Fatalf("expected 4 audit entries (3 actions + 1 scenario_applied), got %d", len(a.entries))
	}
	// First three should be action_executed, last one scenario_applied.
	for i := 0; i < 3; i++ {
		if a.entries[i].Kind != AuditKindActionExecuted {
			t.Errorf("entry %d: expected action_executed, got %s", i, a.entries[i].Kind)
		}
	}
	if a.entries[3].Kind != AuditKindScenarioApplied {
		t.Errorf("expected scenario_applied last, got %s", a.entries[3].Kind)
	}
}

func TestApply_Given_PermissionDenied_When_Apply_Then_Rollback(t *testing.T) {
	r := &stubReader{
		scenarios: map[string]Scenario{
			"s1": {RID: "s1", Status: ScenarioStatusDraft},
		},
		edits: map[string][]ScenarioEdit{
			"s1": {{Op: "ok-a"}, {Op: "denied"}, {Op: "ok-c"}},
		},
	}
	w := &stubWriter{}
	a := &stubAudit{}
	p := &stubPermChecker{deniedOp: "denied"}
	tx := &stubTxRunner{}
	svc := NewServiceWithAudit(r, w, tx, a, p)

	err := svc.Apply(context.Background(), "s1")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("got %v, want ErrPermissionDenied", err)
	}
	// Permission check happens pre-flight (outside the tx), so the tx is
	// never entered — no rollback needed because no writes occurred.
	if tx.rollbackOn != nil {
		t.Errorf("expected no tx entry, got rollback on %v", tx.rollbackOn)
	}
	if len(w.applied) != 0 {
		t.Errorf("expected zero applies on permission failure, got %d", len(w.applied))
	}
	if len(w.stateUpdates) != 0 {
		t.Errorf("scenario not marked applied")
	}
	if len(a.entries) != 0 {
		t.Errorf("expected zero audit entries on pre-flight denial, got %d", len(a.entries))
	}
}
