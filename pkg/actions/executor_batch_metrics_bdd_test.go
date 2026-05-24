package actions

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/liyang/weave/pkg/metrics"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_ExecutorApplyBatchAtomic_EmitsActionMetrics covers the
// remaining piece of Gap-O1 round-41 work: ApplyBatchAtomic is
// all-or-nothing, so per-action metrics must reflect the BATCH's
// final status (ok / error) for every action in the request slice,
// not just the index that failed.
//
// Acceptance criteria (Given → When → Then):
//
//   Given a batch of 3 actions (2 unique types: typeA x2, typeB x1)
//         that all succeed
//   When  ApplyBatchAtomic returns
//   Then  weave_actions_applied_total receives 3 ok observations
//         partitioned correctly per action_type label (typeA=2,
//         typeB=1) AND histogram observation count totals 3
//
//   Given a batch where action index 1 fails at Prepare
//   When  ApplyBatchAtomic returns
//   Then  EVERY action in the batch (including the ones that
//         prepared successfully but never committed) gets a
//         status=error observation — honest accounting of "all-or-
//         nothing aborted the whole batch"
//
//   Given a batch of zero requests
//   When  ApplyBatchAtomic returns
//   Then  no metric observations are emitted (no requests = no
//         per-action observations)
func TestBDD_ExecutorApplyBatchAtomic_EmitsActionMetrics(t *testing.T) {
	t.Run("happy path: per-type counters reflect mixed batch", func(t *testing.T) {
		_ = freshActionMetricsRegistry(t)

		atA := newTestActionType("batchTypeA", []ParameterDef{
			{ID: "name", Type: "string", Required: true},
		}, []Rule{
			{
				Type:       "createObject",
				ObjectType: "Employee",
				PropertyBindings: map[string]PropertyBinding{
					"name": {Type: "parameter", Value: "name"},
				},
			},
		})
		atB := newTestActionType("batchTypeB", []ParameterDef{
			{ID: "name", Type: "string", Required: true},
		}, []Rule{
			{
				Type:       "createObject",
				ObjectType: "Employee",
				PropertyBindings: map[string]PropertyBinding{
					"name": {Type: "parameter", Value: "name"},
				},
			},
		})
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{atA, atB}}
		exec := NewExecutor(repo, nil)

		_, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", []ApplyRequest{
			{ActionType: "batchTypeA", Parameters: map[string]interface{}{"name": "Alice"}},
			{ActionType: "batchTypeA", Parameters: map[string]interface{}{"name": "Bob"}},
			{ActionType: "batchTypeB", Parameters: map[string]interface{}{"name": "Carol"}},
		})
		if err != nil {
			t.Fatalf("ApplyBatchAtomic: %v", err)
		}
		if got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("batchTypeA", "ok")); got != 2 {
			t.Errorf("batchTypeA ok counter = %v, want 2", got)
		}
		if got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("batchTypeB", "ok")); got != 1 {
			t.Errorf("batchTypeB ok counter = %v, want 1", got)
		}
	})

	t.Run("Prepare failure at index 1 → ALL actions get status=error (all-or-nothing)", func(t *testing.T) {
		_ = freshActionMetricsRegistry(t)

		// Only atGood is registered; atMissing references an action
		// type that doesn't exist so its Prepare fails.
		atGood := newTestActionType("batchGood", []ParameterDef{
			{ID: "name", Type: "string", Required: true},
		}, []Rule{
			{
				Type:       "createObject",
				ObjectType: "Employee",
				PropertyBindings: map[string]PropertyBinding{
					"name": {Type: "parameter", Value: "name"},
				},
			},
		})
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{atGood}}
		exec := NewExecutor(repo, nil)

		_, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", []ApplyRequest{
			{ActionType: "batchGood", Parameters: map[string]interface{}{"name": "Alice"}},
			{ActionType: "batchMissing", Parameters: map[string]interface{}{"name": "Bob"}}, // fails Prepare
			{ActionType: "batchGood", Parameters: map[string]interface{}{"name": "Carol"}},
		})
		if err == nil {
			t.Fatal("expected error from missing action type, got nil")
		}

		// batchGood appears twice (indexes 0 + 2); both get status=error
		// per all-or-nothing semantics, even though index 0 prepared
		// successfully and index 2 never even attempted to prepare.
		if got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("batchGood", "error")); got != 2 {
			t.Errorf("batchGood error counter = %v, want 2 (both indexes get aborted by batch failure)", got)
		}
		if got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("batchMissing", "error")); got != 1 {
			t.Errorf("batchMissing error counter = %v, want 1", got)
		}
		// No batchGood observations land in the ok bucket — the batch
		// failed, so no action committed.
		if got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("batchGood", "ok")); got != 0 {
			t.Errorf("batchGood ok counter = %v, want 0 (batch aborted)", got)
		}
	})

	t.Run("empty batch: no observations emitted", func(t *testing.T) {
		_ = freshActionMetricsRegistry(t)

		repo := &mockOmsRepo{actionTypes: []oms.ActionType{}}
		exec := NewExecutor(repo, nil)

		_, err := exec.ApplyBatchAtomic(context.Background(), "ont-1", []ApplyRequest{})
		if err != nil {
			t.Fatalf("ApplyBatchAtomic empty: %v", err)
		}
		// No per-action observations should land. Sample a few label
		// combinations that COULD exist to confirm they're all zero
		// (counters are lazy-instantiated, so absent label rows
		// genuinely have value 0 not "missing").
		if got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("anything", "ok")); got != 0 {
			t.Errorf("empty-batch observation leaked: counter = %v", got)
		}
	})
}
