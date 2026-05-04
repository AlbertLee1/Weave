package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
)

// ParquetFilesTotalForTest returns the current value of the
// weave_parquet_files_total counter for the (ontology, objectType) pair.
// Exported with the ForTest suffix so external packages can assert
// integration-level metric increments without depending on the global
// prometheus testutil shape directly.
func ParquetFilesTotalForTest(ontology, objectType string) float64 {
	return testutil.ToFloat64(parquetFilesTotal.WithLabelValues(ontology, objectType))
}

// ParquetSizeBytesSumForTest returns the histogram _sum value for the
// weave_parquet_size_bytes series scoped to (ontology, objectType). The
// helper walks the Prometheus DTO so the assertion shape stays simple at
// the call site — testutil exposes ToFloat64 only for counters and gauges,
// not for histogram aggregates.
func ParquetSizeBytesSumForTest(ontology, objectType string) float64 {
	return histogramSum(parquetSizeBytes.WithLabelValues(ontology, objectType))
}

// MaterializeLagCountForTest returns the histogram _count value for the
// weave_materialize_lag_seconds series scoped to (ontology, objectType).
func MaterializeLagCountForTest(ontology, objectType string) float64 {
	return histogramCount(materializeLagSeconds.WithLabelValues(ontology, objectType))
}

// histogramSum extracts the _sum aggregate from a single histogram series
// by writing it through the DTO contract that every prometheus.Metric
// implements.
func histogramSum(observer prometheus.Observer) float64 {
	m, ok := observer.(prometheus.Metric)
	if !ok {
		return 0
	}
	var pb dto.Metric
	if err := m.Write(&pb); err != nil {
		return 0
	}
	if pb.Histogram == nil {
		return 0
	}
	return pb.Histogram.GetSampleSum()
}

// histogramCount extracts the _count aggregate from a single histogram
// series via the DTO contract.
func histogramCount(observer prometheus.Observer) float64 {
	m, ok := observer.(prometheus.Metric)
	if !ok {
		return 0
	}
	var pb dto.Metric
	if err := m.Write(&pb); err != nil {
		return 0
	}
	if pb.Histogram == nil {
		return 0
	}
	return float64(pb.Histogram.GetSampleCount())
}
