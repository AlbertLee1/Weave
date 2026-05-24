package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// PRD-V2 §4.6 Gap-O1 calls out weave_objectset_execute_duration_seconds
// as a missing business metric alongside the action / Bleve / funnel
// duration histograms that are already wired. This round closes that
// gap with a HistogramVec keyed by ObjectSet definition kind ("base",
// "filter", "union", "searchAround", …) and outcome (ok | error) so
// the operator can answer "which ObjectSet shape is hot, and is it
// erroring?" from one Grafana panel.
//
// The metric is package-private — production code goes through
// ObserveObjectSetExecute(definitionType, outcome, seconds). The
// accessor is exposed for the same reasons the funnel-side gauges
// expose theirs: prometheus testutil assertions in sibling packages
// and admin-side push-on-write.

func TestBDD_ObjectSetExecuteDuration_GaugeContract(t *testing.T) {
	t.Run("Observe records onto the histogram for the given labels", func(t *testing.T) {
		// Use a unique label combination so prior tests can't leak
		// observations into our bucket count assertion.
		const def = "base-test-contract"
		const outcome = "ok"
		ObserveObjectSetExecute(def, outcome, 0.042)

		h := ObjectSetExecuteDurationHistogram()
		if h == nil {
			t.Fatal("ObjectSetExecuteDurationHistogram() returned nil")
		}
		series, err := h.GetMetricWithLabelValues(def, outcome)
		if err != nil {
			t.Fatalf("GetMetricWithLabelValues: %v", err)
		}
		var pb dto.Metric
		if err := series.(interface{ Write(*dto.Metric) error }).Write(&pb); err != nil {
			t.Fatalf("series.Write: %v", err)
		}
		if pb.Histogram == nil || pb.Histogram.GetSampleCount() < 1 {
			t.Fatalf("expected at least one sample for (%s,%s), got %+v", def, outcome, pb.Histogram)
		}
	})

	t.Run("Negative durations are clamped to zero so a bad clock can't poison the bucket", func(t *testing.T) {
		const def = "base-test-clamp"
		const outcome = "ok"
		ObserveObjectSetExecute(def, outcome, -3.5)

		h := ObjectSetExecuteDurationHistogram()
		series, _ := h.GetMetricWithLabelValues(def, outcome)
		var pb dto.Metric
		_ = series.(interface{ Write(*dto.Metric) error }).Write(&pb)
		if pb.Histogram == nil {
			t.Fatal("histogram missing for clamp test")
		}
		// SampleSum is the sum of all observations. A single -3.5
		// observation would push the sum to -3.5 (or less); after
		// clamping we expect 0.
		if got := pb.Histogram.GetSampleSum(); got < 0 {
			t.Fatalf("sample sum went negative (%v) — clamp guard not enforced", got)
		}
	})

	t.Run("Labels separate definition kinds and outcomes so dashboards can slice", func(t *testing.T) {
		// Pre-populate ok and error series for two distinct kinds so
		// the test asserts the label cardinality the dashboard relies on.
		ObserveObjectSetExecute("base-test-labels-1", "ok", 0.01)
		ObserveObjectSetExecute("base-test-labels-1", "error", 0.02)
		ObserveObjectSetExecute("filter-test-labels-1", "ok", 0.03)

		h := ObjectSetExecuteDurationHistogram()
		for _, lv := range []struct{ def, out string }{
			{"base-test-labels-1", "ok"},
			{"base-test-labels-1", "error"},
			{"filter-test-labels-1", "ok"},
		} {
			s, err := h.GetMetricWithLabelValues(lv.def, lv.out)
			if err != nil {
				t.Fatalf("GetMetricWithLabelValues(%s,%s): %v", lv.def, lv.out, err)
			}
			var pb dto.Metric
			_ = s.(interface{ Write(*dto.Metric) error }).Write(&pb)
			if pb.Histogram == nil || pb.Histogram.GetSampleCount() < 1 {
				t.Errorf("expected at least one sample for (%s,%s), got %+v", lv.def, lv.out, pb.Histogram)
			}
		}
	})

	t.Run("name and labels stay stable for dashboards", func(t *testing.T) {
		// Force one observation, then read back the parent HistogramVec's
		// descriptor via the Describe channel. The descriptor's String()
		// contains the metric Name + Help + variable labels — exactly the
		// surface dashboards rely on.
		ObserveObjectSetExecute("base-test-name", "ok", 0.001)
		descs := make(chan *prometheus.Desc, 4)
		ObjectSetExecuteDurationHistogram().Describe(descs)
		close(descs)
		var combined strings.Builder
		for d := range descs {
			combined.WriteString(d.String())
			combined.WriteByte('\n')
		}
		got := combined.String()
		if !strings.Contains(got, "weave_objectset_execute_duration_seconds") {
			t.Fatalf("expected metric name in descriptor, got %s", got)
		}
		if !strings.Contains(got, "definition_type") || !strings.Contains(got, "outcome") {
			t.Fatalf("expected definition_type + outcome labels in descriptor, got %s", got)
		}
	})
}
