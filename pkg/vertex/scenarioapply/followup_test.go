package scenarioapply

import (
	"context"
	"errors"
	"testing"
)

type stubActionRunner struct {
	executed []FollowUpAction
	failOn   string
}

func (s *stubActionRunner) ExecuteAction(_ context.Context, a FollowUpAction) error {
	s.executed = append(s.executed, a)
	if s.failOn == a.ActionTypeRID {
		return errors.New("action " + a.ActionTypeRID + " failed")
	}
	return nil
}

func TestApplyWithFollowUps_Given_2FollowUps_When_AllSucceed_Then_BothExecuted(t *testing.T) {
	r := &stubReader{
		scenarios: map[string]Scenario{
			"s1": {RID: "s1", Status: ScenarioStatusDraft},
		},
		edits: map[string][]ScenarioEdit{
			"s1": {{Op: "a"}},
		},
	}
	w := &stubWriter{}
	a := &stubAudit{}
	p := &stubPermChecker{}
	runner := &stubActionRunner{}
	svc := NewServiceWithAudit(r, w, &stubTxRunner{}, a, p).WithActionRunner(runner)

	err := svc.ApplyWithFollowUps(context.Background(), "s1", []FollowUpAction{
		{ActionTypeRID: "ri.actions.main.action-type.notify", Params: map[string]any{"to": "ops"}},
		{ActionTypeRID: "ri.actions.main.action-type.archive", Params: map[string]any{}},
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(runner.executed) != 2 {
		t.Errorf("expected 2 actions executed, got %d", len(runner.executed))
	}
	// Audit log: 1 action_executed (edit) + 1 scenario_applied + 2
	// followup_executed.
	if len(a.entries) != 4 {
		t.Errorf("expected 4 audit entries (1 edit + 1 applied + 2 followups), got %d", len(a.entries))
	}
}

func TestApplyWithFollowUps_Given_FollowUpFails_When_Apply_Then_ApplyAlreadyCommittedAndFollowUpAuditAsFailed(t *testing.T) {
	r := &stubReader{
		scenarios: map[string]Scenario{
			"s1": {RID: "s1", Status: ScenarioStatusDraft},
		},
		edits: map[string][]ScenarioEdit{
			"s1": {{Op: "a"}},
		},
	}
	w := &stubWriter{}
	a := &stubAudit{}
	p := &stubPermChecker{}
	runner := &stubActionRunner{failOn: "ri.actions.main.action-type.bad"}
	svc := NewServiceWithAudit(r, w, &stubTxRunner{}, a, p).WithActionRunner(runner)

	err := svc.ApplyWithFollowUps(context.Background(), "s1", []FollowUpAction{
		{ActionTypeRID: "ri.actions.main.action-type.ok"},
		{ActionTypeRID: "ri.actions.main.action-type.bad"},
		{ActionTypeRID: "ri.actions.main.action-type.untouched"},
	})
	if err != nil {
		t.Errorf("apply itself should succeed, got %v", err)
	}
	// Apply phase did commit (scenario marked applied + event published).
	if len(w.stateUpdates) != 1 {
		t.Errorf("expected scenario marked applied even though followup failed")
	}
	if len(w.published) != 1 {
		t.Errorf("expected publish, got %d", len(w.published))
	}
	// Follow-ups: ok ran, bad ran (and failed), untouched skipped.
	if len(runner.executed) != 2 {
		t.Errorf("expected 2 followups attempted (stop on first fail), got %d", len(runner.executed))
	}
	// Audit log must contain the failed-followup row.
	foundFailed := false
	for _, entry := range a.entries {
		if entry.Kind == AuditKindFollowUpFailed && entry.Op == "ri.actions.main.action-type.bad" {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Errorf("expected followup_failed audit row for the failing action")
	}
}

func TestApplyWithFollowUps_Given_NoFollowUps_When_Apply_Then_BehavesLikePlainApply(t *testing.T) {
	r := &stubReader{
		scenarios: map[string]Scenario{"s1": {RID: "s1", Status: ScenarioStatusDraft}},
		edits:     map[string][]ScenarioEdit{"s1": {{Op: "a"}}},
	}
	w := &stubWriter{}
	a := &stubAudit{}
	p := &stubPermChecker{}
	svc := NewServiceWithAudit(r, w, &stubTxRunner{}, a, p).WithActionRunner(&stubActionRunner{})

	if err := svc.ApplyWithFollowUps(context.Background(), "s1", nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// 1 action_executed (edit) + 1 scenario_applied
	if len(a.entries) != 2 {
		t.Errorf("expected 2 audit entries, got %d", len(a.entries))
	}
}
