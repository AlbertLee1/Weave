package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Per-ontology cost-tracking metrics (US-447).
//
// The four dimensions surface the operational cost of each ontology so
// runtime dashboards can answer "which ontology is dominating disk /
// compute / message volume / row count" without per-row joins:
//
//   - weave_cost_storage_bytes_total{ontology, kind} — counter of bytes
//     written to durable columnar storage. `kind` discriminates the
//     storage tier (currently only "parquet" via the materializer; future
//     tiers like "archive" or "snapshot" plug in by passing a new value).
//   - weave_cost_cpu_seconds_total{ontology, op} — counter of wall-clock
//     CPU seconds spent on ontology-tagged work. `op` discriminates the
//     producer (currently "apply_batch"); other producers (action exec,
//     SQL exec) plug in via the same helper.
//   - weave_cost_nats_messages_total{ontology} — counter of NATS messages
//     processed for an ontology (one per applied batch).
//   - weave_cost_pg_rows{ontology, table} — gauge of PostgreSQL row counts
//     observed by a periodic poller. `table` discriminates the PG table.
//
// All four metrics keep ontology as the primary label so a single
// templated dashboard variable drives every panel without per-metric
// label maps. The `kind` / `op` / `table` sub-labels are kept low-
// cardinality (a fixed set per producer) so per-ontology series counts
// stay bounded as the ontology catalogue grows.
var (
	costStorageBytesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_cost_storage_bytes_total",
			Help: "Cumulative bytes written to durable storage tiers, partitioned by ontology and storage kind.",
		},
		[]string{"ontology", "kind"},
	)
	costCPUSecondsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_cost_cpu_seconds_total",
			Help: "Cumulative wall-clock CPU seconds spent on ontology-tagged work, partitioned by ontology and producer op.",
		},
		[]string{"ontology", "op"},
	)
	costNATSMessagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weave_cost_nats_messages_total",
			Help: "Cumulative NATS messages processed for an ontology, partitioned by ontology.",
		},
		[]string{"ontology"},
	)
	costPGRows = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "weave_cost_pg_rows",
			Help: "Observed PostgreSQL row counts per ontology and table, refreshed by the periodic cost poller.",
		},
		[]string{"ontology", "table"},
	)
)

// Storage kind constants — keep low cardinality. Add new constants here
// when a new storage tier joins the cost model.
const (
	CostStorageKindParquet = "parquet"
)

// CPU op constants — keep low cardinality. Add new constants here when a
// new producer joins the cost model.
const (
	CostCPUOpApplyBatch = "apply_batch"
)

// PG table constants — keep low cardinality. Add new constants here when
// a new table joins the cost poller's scope.
const (
	CostPGTableObjectHistory       = "object_history"
	CostPGTableDatasetTransactions = "dataset_transactions"
)

// RecordOntologyStorageBytes increments the per-ontology storage counter
// by the supplied byte count. Negative or zero byte values are ignored
// so a fail-closed metric source can pass an unverified file size
// without polluting the counter. Empty ontology / kind labels are
// dropped silently — recording would create unbounded "" series under
// either label and the dashboard's templating layer can't filter them
// out.
func RecordOntologyStorageBytes(ontology, kind string, bytes int64) {
	if ontology == "" || kind == "" || bytes <= 0 {
		return
	}
	costStorageBytesTotal.WithLabelValues(ontology, kind).Add(float64(bytes))
}

// RecordOntologyCPUSeconds increments the per-ontology CPU counter by
// the supplied duration in seconds. Negative or zero durations are
// ignored. Empty ontology / op labels are dropped silently — same
// rationale as RecordOntologyStorageBytes.
func RecordOntologyCPUSeconds(ontology, op string, d time.Duration) {
	if ontology == "" || op == "" {
		return
	}
	seconds := d.Seconds()
	if seconds <= 0 {
		return
	}
	costCPUSecondsTotal.WithLabelValues(ontology, op).Add(seconds)
}

// RecordOntologyNATSMessage increments the per-ontology NATS message
// counter by one. Empty ontology label is dropped silently.
func RecordOntologyNATSMessage(ontology string) {
	if ontology == "" {
		return
	}
	costNATSMessagesTotal.WithLabelValues(ontology).Inc()
}

// SetOntologyPGRows sets the gauge value for an (ontology, table) pair
// to the supplied row count. Negative values are clamped to zero so a
// transient query failure cannot drive a panel into negative territory.
// Empty ontology / table labels are dropped silently. This is the only
// per-ontology metric helper that uses Set rather than Add because the
// PG row count is a snapshot (not a delta) refreshed by a periodic
// poller.
func SetOntologyPGRows(ontology, table string, rows float64) {
	if ontology == "" || table == "" {
		return
	}
	if rows < 0 {
		rows = 0
	}
	costPGRows.WithLabelValues(ontology, table).Set(rows)
}

// ResetOntologyCostMetrics clears every per-ontology cost metric. Tests
// use it to isolate observations between table-driven cases without
// reaching into the package-private CounterVec / GaugeVec values.
func ResetOntologyCostMetrics() {
	costStorageBytesTotal.Reset()
	costCPUSecondsTotal.Reset()
	costNATSMessagesTotal.Reset()
	costPGRows.Reset()
}
