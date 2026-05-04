package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestMaterializeFileWritten_RegistersAllThreeMetrics confirms that the
// US-409 metrics surface (lag histogram, files counter, size histogram) is
// fully populated on a single observation and round-trips through the
// shared Register() pipeline.
func TestMaterializeFileWritten_RegistersAllThreeMetrics(t *testing.T) {
	r := NewRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}

	parquetFilesTotal.Reset()
	parquetSizeBytes.Reset()
	materializeLagSeconds.Reset()

	MaterializeFileWritten("northwind", "Customer", 250*time.Millisecond, 1024)

	mfs, err := r.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	have := map[string]bool{}
	for _, mf := range mfs {
		have[mf.GetName()] = true
	}

	required := []string{
		"weave_materialize_lag_seconds",
		"weave_parquet_files_total",
		"weave_parquet_size_bytes",
	}
	for _, name := range required {
		if !have[name] {
			t.Errorf("expected metric %q to be registered, missing", name)
		}
	}
}

// TestMaterializeFileWritten_CounterIncrement validates that the per-call
// increment on weave_parquet_files_total is exactly 1 and that the labels
// are projected through unchanged. Two distinct (ontology, object_type)
// pairs must accumulate independently.
func TestMaterializeFileWritten_CounterIncrement(t *testing.T) {
	parquetFilesTotal.Reset()
	parquetSizeBytes.Reset()
	materializeLagSeconds.Reset()

	MaterializeFileWritten("northwind", "Customer", 100*time.Millisecond, 512)
	MaterializeFileWritten("northwind", "Customer", 200*time.Millisecond, 1024)
	MaterializeFileWritten("northwind", "Order", 50*time.Millisecond, 256)

	if got := testutil.ToFloat64(parquetFilesTotal.WithLabelValues("northwind", "Customer")); got != 2 {
		t.Errorf("Customer files counter = %v, want 2", got)
	}
	if got := testutil.ToFloat64(parquetFilesTotal.WithLabelValues("northwind", "Order")); got != 1 {
		t.Errorf("Order files counter = %v, want 1", got)
	}
}

// TestMaterializeFileWritten_LagClampedNonNegative confirms a negative lag
// (clock skew, pinned timestamps from tests pre-dating the call) is clamped
// to zero before observation so the histogram never contains negative
// buckets that would silently break a Grafana p99 query.
func TestMaterializeFileWritten_LagClampedNonNegative(t *testing.T) {
	r := NewRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	parquetFilesTotal.Reset()
	parquetSizeBytes.Reset()
	materializeLagSeconds.Reset()

	MaterializeFileWritten("northwind", "Customer", -5*time.Second, 1024)

	expected := `
# HELP weave_materialize_lag_seconds Wall-clock delta between an edit batch's Timestamp and the moment its parquet file is written, partitioned by ontology and object type.
# TYPE weave_materialize_lag_seconds histogram
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="0.05"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="0.1"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="0.25"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="0.5"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="1"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="2.5"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="5"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="10"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="30"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="60"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="300"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="600"} 1
weave_materialize_lag_seconds_bucket{object_type="Customer",ontology="northwind",le="+Inf"} 1
weave_materialize_lag_seconds_sum{object_type="Customer",ontology="northwind"} 0
weave_materialize_lag_seconds_count{object_type="Customer",ontology="northwind"} 1
`
	if err := testutil.GatherAndCompare(r, strings.NewReader(expected), "weave_materialize_lag_seconds"); err != nil {
		t.Fatalf("lag clamp compare: %v", err)
	}
}

// TestMaterializeFileWritten_SizeHistogramSumIsCumulative verifies the
// _sum series accumulates byte totals across observations — the dashboard
// derives Bps from the rate of this series, so a regression to "last value
// only" semantics would silently halve the cumulative bytes panel.
func TestMaterializeFileWritten_SizeHistogramSumIsCumulative(t *testing.T) {
	parquetFilesTotal.Reset()
	parquetSizeBytes.Reset()
	materializeLagSeconds.Reset()

	MaterializeFileWritten("northwind", "Customer", time.Second, 100)
	MaterializeFileWritten("northwind", "Customer", time.Second, 250)
	MaterializeFileWritten("northwind", "Customer", time.Second, 1000)

	if got := testutil.CollectAndCount(parquetSizeBytes); got == 0 {
		t.Fatalf("parquet size histogram has no recorded series")
	}
	wantSum := float64(100 + 250 + 1000)
	if got := testutil.CollectAndCount(parquetSizeBytes); got != 1 {
		t.Errorf("expected 1 distinct (ontology, object_type) series, got %d", got)
	}
	expected := `
# HELP weave_parquet_size_bytes Per-file parquet payload size in bytes (histogram _sum exposes cumulative bytes), partitioned by ontology and object type.
# TYPE weave_parquet_size_bytes histogram
`
	r := NewRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	mfs, err := r.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "weave_parquet_size_bytes" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if got := m.GetHistogram().GetSampleSum(); got != wantSum {
				t.Errorf("histogram _sum = %v, want %v", got, wantSum)
			}
		}
	}
	_ = expected
}

// TestMaterializeFileWritten_SkipsZeroSize confirms a zero-byte file (stat
// failure swallowed by writeFile) does NOT pollute the size histogram with
// a 0-byte sample but DOES still increment the files counter. The lag
// observation also fires regardless of size because lag is independent of
// payload bytes.
func TestMaterializeFileWritten_SkipsZeroSize(t *testing.T) {
	parquetFilesTotal.Reset()
	parquetSizeBytes.Reset()
	materializeLagSeconds.Reset()

	MaterializeFileWritten("northwind", "Customer", 100*time.Millisecond, 0)

	if got := testutil.ToFloat64(parquetFilesTotal.WithLabelValues("northwind", "Customer")); got != 1 {
		t.Errorf("files counter = %v, want 1 (zero size still counts as a file)", got)
	}
	if got := testutil.CollectAndCount(parquetSizeBytes); got != 0 {
		t.Errorf("expected 0 size histogram series for zero-byte file, got %d", got)
	}
}
