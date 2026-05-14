package metrics

import (
	"testing"
)

// TestVertexMetrics_Given_FreshRegistry_When_Observed_Then_AllFiveExposed
// is the VTX-100 gate: it requires the five vertex_* metrics named in the
// PRD acceptance criteria to be observable through Gather. Without these
// names a future PromQL dashboard would break silently.
func TestVertexMetrics_Given_FreshRegistry_When_Observed_Then_AllFiveExposed(t *testing.T) {
	r := NewRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Touch each metric so it shows up in Gather (label-less *Vec
	// collectors are otherwise omitted).
	ObserveVertexScenarioRun("ok", 0.123)
	ObserveVertexOverlayFold(0.001)
	SetVertexGraphsTotal(7)
	ObserveVertexFunctionInvocation("propagateDelay", "ok")

	mfs, err := r.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	have := map[string]bool{}
	for _, mf := range mfs {
		have[mf.GetName()] = true
	}

	required := []string{
		"vertex_scenario_runs_total",
		"vertex_scenario_run_duration_seconds",
		"vertex_overlay_fold_duration_seconds",
		"vertex_graphs_total",
		"vertex_function_invocations_total",
	}
	for _, name := range required {
		if !have[name] {
			t.Errorf("missing required vertex metric %q", name)
		}
	}
}
