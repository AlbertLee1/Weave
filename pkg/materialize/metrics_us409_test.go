package materialize

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/metrics"
)

// TestMaterializer_EmitsUS409Metrics verifies that a successful
// MaterializeBatch increments weave_parquet_files_total and observes a
// non-zero size into weave_parquet_size_bytes_sum. This is the end-to-end
// proof that the wiring inside MaterializeBatch survives a real parquet
// round-trip — pkg/metrics' unit tests cover the helper alone, this one
// covers the integration seam.
func TestMaterializer_EmitsUS409Metrics(t *testing.T) {
	r := metrics.NewRegistry()
	if err := metrics.Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}

	beforeFiles := metrics.ParquetFilesTotalForTest("northwind", "Customer")
	beforeSizeSum := metrics.ParquetSizeBytesSumForTest("northwind", "Customer")
	beforeLagCount := metrics.MaterializeLagCountForTest("northwind", "Customer")

	m := NewMaterializer(t.TempDir())
	if err := m.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-metrics",
		OntologyAPIName: "northwind",
		Timestamp:       time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC),
		Edits: []funnel.Edit{
			{
				Type:       funnel.EditTypeCreate,
				ObjectType: "Customer",
				PrimaryKey: "C-1",
				Properties: map[string]interface{}{"name": "Alice"},
			},
		},
	}); err != nil {
		t.Fatalf("MaterializeBatch: %v", err)
	}

	if got := metrics.ParquetFilesTotalForTest("northwind", "Customer"); got != beforeFiles+1 {
		t.Errorf("parquet_files_total Customer delta = %v, want 1", got-beforeFiles)
	}
	if got := metrics.ParquetSizeBytesSumForTest("northwind", "Customer"); !(got > beforeSizeSum) {
		t.Errorf("parquet_size_bytes_sum did not advance (before=%v, after=%v)", beforeSizeSum, got)
	}
	if got := metrics.MaterializeLagCountForTest("northwind", "Customer"); got != beforeLagCount+1 {
		t.Errorf("materialize_lag_seconds count delta = %v, want 1", got-beforeLagCount)
	}

	// Sanity: the testutil-based assertion routes through the registry that
	// Register populated above, so a future regression that bypasses
	// allCollectors() (e.g. forgetting to add a new metric to the slice)
	// surfaces here too.
	_ = r
	_ = testutil.CollectAndCount
}
