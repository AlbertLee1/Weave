package metrics

import "github.com/prometheus/client_golang/prometheus"

// ActionsAppliedCounterFor returns the counter row for the given
// (action_type, status) label pair. Exported as a test helper so
// downstream packages (pkg/actions BDD) can scrape per-label values
// via testutil.ToFloat64 without re-implementing the label lookup.
//
// Production code calls metrics.ActionApplied(...); this helper exists
// only for instrumentation acceptance tests.
func ActionsAppliedCounterFor(actionType, status string) prometheus.Counter {
	return actionsAppliedTotal.WithLabelValues(actionType, status)
}

// ResetActionMetricsForTest zeros the weave_actions_applied_total +
// weave_actions_duration_seconds vectors so each test in the
// pkg/actions metrics suite starts from a clean baseline. The
// pkg/metrics globals are process-wide; without this reset, parallel
// tests would observe each other's increments. Production callers
// MUST NOT use this helper.
func ResetActionMetricsForTest() {
	actionsAppliedTotal.Reset()
	actionsDuration.Reset()
}
