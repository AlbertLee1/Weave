package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// PRD-V2 §4.6 Gap-O1 — weave_objectset_execute_duration_seconds.
//
// This histogram completes the OSv2-alignment observability surface
// alongside the action / Bleve / funnel duration histograms already in
// metrics.go. Operators can answer "which ObjectSet shape is hot, and
// is it erroring?" from one Grafana panel by sum/rate-ing over the
// (definition_type, outcome) label pair.
//
// Cardinality: definition_type is bounded by Executor.execute's switch
// statement (~15 kinds: base/filter/union/intersect/subtract/searchAround/
// reference/nearestNeighbors/withProperties/static/asType/asBaseObjectTypes/
// interfaceBase/interfaceLinkSearchAround/sample); outcome is a closed
// {ok, error} pair. Total series ≈ 30 — well under any sane budget.

var objectSetExecuteDuration = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "weave_objectset_execute_duration_seconds",
		Help:    "ObjectSet Executor.Execute latency in seconds, partitioned by definition_type (base, filter, union, …) and outcome (ok|error).",
		Buckets: prometheus.DefBuckets,
	},
	[]string{"definition_type", "outcome"},
)

// ObserveObjectSetExecute records one Executor.Execute call's wall-clock
// duration onto the histogram. Negative durations are clamped to zero so a
// bad system clock or a goroutine scheduling artefact can't poison the
// bucket; this matches the defensive shape SetFunnelConsumerLag uses.
//
// definitionType is the ObjectSet kind from Definition.Type — one of
// the strings in Executor.execute's switch (no need to keep a parallel
// list; the executor passes them through verbatim).
//
// outcome is "ok" for nil errors and "error" otherwise. Production
// wiring lives in pkg/oss/objectset/executor.go (see Execute's deferred
// observe call).
func ObserveObjectSetExecute(definitionType, outcome string, seconds float64) {
	if seconds < 0 {
		seconds = 0
	}
	objectSetExecuteDuration.WithLabelValues(definitionType, outcome).Observe(seconds)
}

// ObjectSetExecuteDurationHistogram exposes the package-private vec so
// sibling packages (tests, admin handlers) can assert against its
// observed values via prometheus testutil. Production code should NOT
// use this directly — always go through ObserveObjectSetExecute.
func ObjectSetExecuteDurationHistogram() *prometheus.HistogramVec {
	return objectSetExecuteDuration
}
