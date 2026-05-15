package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Vertex metrics (VTX-100). Naming intentionally uses the `vertex_` prefix
// (not `weave_`) so the dashboard wired in grafana/vertex-dashboard.json
// can filter by metric prefix without enumerating individual names. All
// five names are listed in the PRD acceptance criteria; do not rename or
// the dashboard panels break silently.
var (
	vertexScenarioRunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vertex_scenario_runs_total",
			Help: "Total Vertex Scenario runs, partitioned by ok|error status.",
		},
		[]string{"status"},
	)
	vertexScenarioRunDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "vertex_scenario_run_duration_seconds",
			Help:    "End-to-end Vertex Scenario run duration in seconds.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 300},
		},
		[]string{"status"},
	)
	vertexOverlayFoldDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "vertex_overlay_fold_duration_seconds",
			Help: "Per-call wall time of scenarios.FoldObject / FoldLinks in seconds.",
			// Tighter buckets than the run histogram: fold should be sub-ms
			// at low edit counts and the alert threshold is single-digit ms.
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
		},
	)
	vertexGraphsTotal = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "vertex_graphs_total",
			Help: "Current number of saved Vertex System Graphs.",
		},
	)
	vertexFunctionInvocations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "vertex_function_invocations_total",
			Help: "Total invocations of Vertex Function-backed actions, partitioned by function name and ok|error status.",
		},
		[]string{"function", "status"},
	)
)

// ObserveVertexScenarioRun records one Scenario run with its outcome and
// wall-clock duration. Call from the Run executor on every job completion.
func ObserveVertexScenarioRun(status string, durationSeconds float64) {
	vertexScenarioRunsTotal.WithLabelValues(status).Inc()
	vertexScenarioRunDuration.WithLabelValues(status).Observe(durationSeconds)
}

// ObserveVertexScenarioRunDuration is the time.Duration sibling of
// ObserveVertexScenarioRun — callers timing with time.Since(start) get a
// helper that does the unit conversion in one place.
func ObserveVertexScenarioRunDuration(status string, d time.Duration) {
	ObserveVertexScenarioRun(status, d.Seconds())
}

// ObserveVertexOverlayFold records one FoldObject / FoldLinks invocation.
// The histogram has no labels — the perf budget in VTX-098 is per-fold and
// independent of which object / scenario triggered it.
func ObserveVertexOverlayFold(durationSeconds float64) {
	vertexOverlayFoldDuration.Observe(durationSeconds)
}

// ObserveVertexOverlayFoldDuration is the time.Duration sibling.
func ObserveVertexOverlayFoldDuration(d time.Duration) {
	ObserveVertexOverlayFold(d.Seconds())
}

// SetVertexGraphsTotal records the current count of saved Vertex graphs.
// Call from a periodic reconciler (or after CRUD writes) — Prometheus
// scrapes are pull-based so the value just needs to be fresh at scrape
// time, not on every change.
func SetVertexGraphsTotal(n int) {
	vertexGraphsTotal.Set(float64(n))
}

// ObserveVertexFunctionInvocation increments the counter for one
// Function-backed Action invocation.
func ObserveVertexFunctionInvocation(function, status string) {
	vertexFunctionInvocations.WithLabelValues(function, status).Inc()
}

// vertexCollectors returns the five vertex_* collectors in stable order.
// Kept separate from allCollectors() so adding a new vertex metric does
// not force a re-read of the entire metrics inventory.
func vertexCollectors() []prometheus.Collector {
	return []prometheus.Collector{
		vertexScenarioRunsTotal,
		vertexScenarioRunDuration,
		vertexOverlayFoldDuration,
		vertexGraphsTotal,
		vertexFunctionInvocations,
	}
}
