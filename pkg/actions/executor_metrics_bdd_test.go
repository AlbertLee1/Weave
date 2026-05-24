package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/liyang/weave/pkg/metrics"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_ExecutorApply_EmitsActionMetrics covers PRD-V2 Gap-O1
// gap-of-record: the histogram weave_actions_duration_seconds +
// counter weave_actions_applied_total were registered in
// pkg/metrics and have a public ActionApplied(actionType, err,
// duration) helper but no executor code called the helper —
// metrics observed zero in production despite Prometheus seeing
// the metric definitions.
//
// Round 41 wires metrics.ActionApplied into Executor.Apply so the
// counter + histogram actually fire on every action.
//
// Acceptance criteria (Given → When → Then):
//
//   Given an Executor + a successful Apply call for action type X
//   When  we scrape the prometheus registry
//   Then  weave_actions_applied_total{action_type="X",status="ok"}
//         increments by 1
//
//   Given an Executor + a failing Apply call (unknown action)
//   When  we scrape the prometheus registry
//   Then  weave_actions_applied_total{action_type="X",status="error"}
//         increments by 1
//
//   Given an Executor + multiple Apply calls for two different
//         action types
//   When  we scrape the prometheus registry
//   Then  the per-type counters are independent
func TestBDD_ExecutorApply_EmitsActionMetrics(t *testing.T) {
	t.Run("successful Apply increments status=ok counter", func(t *testing.T) {
		r := freshActionMetricsRegistry(t)
		_ = r // kept so the registry doesn't get GC'd before the assertions

		at := newTestActionType("metricSuccess", []ParameterDef{
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
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{at}}
		exec := NewExecutor(repo, nil)

		_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "metricSuccess",
			Parameters: map[string]interface{}{"name": "Alice"},
		})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}

		got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("metricSuccess", "ok"))
		if got != 1 {
			t.Errorf("weave_actions_applied_total{action_type=metricSuccess, status=ok} = %v, want 1", got)
		}
		// Histogram observation count should match.
		count := histogramObservationCount(t, r, "metricSuccess")
		if count != 1 {
			t.Errorf("weave_actions_duration_seconds count = %d, want 1", count)
		}
	})

	t.Run("failing Apply (unknown action type) increments status=error counter", func(t *testing.T) {
		r := freshActionMetricsRegistry(t)
		_ = r

		repo := &mockOmsRepo{actionTypes: []oms.ActionType{}}
		exec := NewExecutor(repo, nil)

		_, err := exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "ghost",
		})
		if err == nil {
			t.Fatal("Apply: expected error for unknown action type, got nil")
		}

		got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("ghost", "error"))
		if got != 1 {
			t.Errorf("weave_actions_applied_total{action_type=ghost, status=error} = %v, want 1", got)
		}
	})

	t.Run("multiple Apply calls increment per-type counters independently", func(t *testing.T) {
		_ = freshActionMetricsRegistry(t)

		at1 := newTestActionType("multA", []ParameterDef{
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
		at2 := newTestActionType("multB", []ParameterDef{
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
		repo := &mockOmsRepo{actionTypes: []oms.ActionType{at1, at2}}
		exec := NewExecutor(repo, nil)

		for i := 0; i < 3; i++ {
			_, _ = exec.Apply(context.Background(), "ont-1", &ApplyRequest{
				ActionType: "multA",
				Parameters: map[string]interface{}{"name": "Alice"},
			})
		}
		_, _ = exec.Apply(context.Background(), "ont-1", &ApplyRequest{
			ActionType: "multB",
			Parameters: map[string]interface{}{"name": "Bob"},
		})

		if got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("multA", "ok")); got != 3 {
			t.Errorf("multA ok counter = %v, want 3", got)
		}
		if got := testutil.ToFloat64(metrics.ActionsAppliedCounterFor("multB", "ok")); got != 1 {
			t.Errorf("multB ok counter = %v, want 1", got)
		}
	})
}

// freshActionMetricsRegistry returns a new prometheus.Registry with
// every Weave metric registered, and resets the global counter/
// histogram pkg-level vars so each sub-test starts at zero. The
// pkg/metrics globals are process-wide; tests use
// metrics.ResetActionMetricsForTest to avoid cross-test contamination.
func freshActionMetricsRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()
	r := metrics.NewRegistry()
	if err := metrics.Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	metrics.ResetActionMetricsForTest()
	return r
}

// histogramObservationCount returns the total observation count for
// weave_actions_duration_seconds{action_type=X} across all bucket
// rows in the gathered metric families.
func histogramObservationCount(t *testing.T, r *prometheus.Registry, actionType string) int {
	t.Helper()
	mfs, err := r.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "weave_actions_duration_seconds" {
			continue
		}
		for _, m := range mf.GetMetric() {
			matches := false
			for _, l := range m.GetLabel() {
				if l.GetName() == "action_type" && l.GetValue() == actionType {
					matches = true
					break
				}
			}
			if !matches {
				continue
			}
			h := m.GetHistogram()
			if h == nil {
				return 0
			}
			return int(h.GetSampleCount())
		}
	}
	return 0
}

// Force-use of strings to keep the import honest in case downstream
// edits drop the only usage.
var _ = strings.Contains
