package metrics

import (
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestRecordOntologyStorageBytes_RegistersAndIncrements confirms the
// US-447 storage counter exposes the canonical metric name + ontology /
// kind labels and that two observations sum to the expected total.
func TestRecordOntologyStorageBytes_RegistersAndIncrements(t *testing.T) {
	r := NewRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ResetOntologyCostMetrics()

	RecordOntologyStorageBytes("northwind", CostStorageKindParquet, 1024)
	RecordOntologyStorageBytes("northwind", CostStorageKindParquet, 512)

	got := testutil.ToFloat64(costStorageBytesTotal.WithLabelValues("northwind", CostStorageKindParquet))
	if got != 1536 {
		t.Errorf("storage counter = %v, want 1536", got)
	}

	mfs, err := r.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	have := map[string]bool{}
	for _, mf := range mfs {
		have[mf.GetName()] = true
	}
	if !have["weave_cost_storage_bytes_total"] {
		t.Error("expected weave_cost_storage_bytes_total to be registered")
	}
}

// TestRecordOntologyStorageBytes_DropsInvalidObservations confirms the
// helper silently no-ops on empty labels and non-positive byte values
// rather than polluting the surface with "" / negative series.
func TestRecordOntologyStorageBytes_DropsInvalidObservations(t *testing.T) {
	ResetOntologyCostMetrics()

	RecordOntologyStorageBytes("", CostStorageKindParquet, 100)
	RecordOntologyStorageBytes("northwind", "", 100)
	RecordOntologyStorageBytes("northwind", CostStorageKindParquet, 0)
	RecordOntologyStorageBytes("northwind", CostStorageKindParquet, -1)

	got := testutil.ToFloat64(costStorageBytesTotal.WithLabelValues("northwind", CostStorageKindParquet))
	if got != 0 {
		t.Errorf("invalid observations leaked into counter; got %v want 0", got)
	}
}

// TestRecordOntologyCPUSeconds_AccumulatesDurations confirms that two
// duration observations sum on the (ontology, op) pair and that
// non-positive durations are ignored.
func TestRecordOntologyCPUSeconds_AccumulatesDurations(t *testing.T) {
	ResetOntologyCostMetrics()

	RecordOntologyCPUSeconds("chinook", CostCPUOpApplyBatch, 100*time.Millisecond)
	RecordOntologyCPUSeconds("chinook", CostCPUOpApplyBatch, 250*time.Millisecond)
	// Non-positive durations and empty labels: no-op.
	RecordOntologyCPUSeconds("chinook", CostCPUOpApplyBatch, 0)
	RecordOntologyCPUSeconds("chinook", CostCPUOpApplyBatch, -1*time.Second)
	RecordOntologyCPUSeconds("", CostCPUOpApplyBatch, 1*time.Second)
	RecordOntologyCPUSeconds("chinook", "", 1*time.Second)

	got := testutil.ToFloat64(costCPUSecondsTotal.WithLabelValues("chinook", CostCPUOpApplyBatch))
	want := 0.35
	if abs(got-want) > 1e-9 {
		t.Errorf("cpu counter = %v, want %v", got, want)
	}
}

// TestRecordOntologyNATSMessage_PerOntologyIsolation confirms that two
// distinct ontologies contribute to independent series and an empty
// ontology label is dropped.
func TestRecordOntologyNATSMessage_PerOntologyIsolation(t *testing.T) {
	ResetOntologyCostMetrics()

	RecordOntologyNATSMessage("northwind")
	RecordOntologyNATSMessage("northwind")
	RecordOntologyNATSMessage("chinook")
	RecordOntologyNATSMessage("") // dropped

	if got := testutil.ToFloat64(costNATSMessagesTotal.WithLabelValues("northwind")); got != 2 {
		t.Errorf("northwind nats counter = %v, want 2", got)
	}
	if got := testutil.ToFloat64(costNATSMessagesTotal.WithLabelValues("chinook")); got != 1 {
		t.Errorf("chinook nats counter = %v, want 1", got)
	}
}

// TestSetOntologyPGRows_ClampsNegativeAndDropsEmptyLabels confirms the
// gauge clamps a negative reading to zero (a transient query failure
// must never drive a panel below the x-axis) and silently drops empty
// ontology / table labels.
func TestSetOntologyPGRows_ClampsNegativeAndDropsEmptyLabels(t *testing.T) {
	ResetOntologyCostMetrics()

	SetOntologyPGRows("northwind", CostPGTableObjectHistory, 1234)
	SetOntologyPGRows("northwind", CostPGTableObjectHistory, -1)
	SetOntologyPGRows("", CostPGTableObjectHistory, 99)
	SetOntologyPGRows("northwind", "", 99)

	if got := testutil.ToFloat64(costPGRows.WithLabelValues("northwind", CostPGTableObjectHistory)); got != 0 {
		t.Errorf("negative reading must clamp to 0; got %v", got)
	}

	SetOntologyPGRows("chinook", CostPGTableDatasetTransactions, 42)
	if got := testutil.ToFloat64(costPGRows.WithLabelValues("chinook", CostPGTableDatasetTransactions)); got != 42 {
		t.Errorf("chinook gauge = %v, want 42", got)
	}
}

// TestCostMetrics_AppearInGather confirms all four US-447 metric names
// appear in a fresh Register pipeline — protects against a future
// allCollectors() refactor that drops one of them.
func TestCostMetrics_AppearInGather(t *testing.T) {
	r := NewRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ResetOntologyCostMetrics()

	// Touch each metric so the Gather output includes them.
	RecordOntologyStorageBytes("o", CostStorageKindParquet, 1)
	RecordOntologyCPUSeconds("o", CostCPUOpApplyBatch, time.Millisecond)
	RecordOntologyNATSMessage("o")
	SetOntologyPGRows("o", CostPGTableObjectHistory, 1)

	mfs, err := r.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	have := map[string]bool{}
	for _, mf := range mfs {
		have[mf.GetName()] = true
	}
	required := []string{
		"weave_cost_storage_bytes_total",
		"weave_cost_cpu_seconds_total",
		"weave_cost_nats_messages_total",
		"weave_cost_pg_rows",
	}
	for _, name := range required {
		if !have[name] {
			t.Errorf("expected metric %q to be registered, missing", name)
		}
	}
}

// TestCostMetrics_HelpStringsMentionOntology pins the documentation
// surface so a future help-text edit doesn't accidentally drop the
// "ontology" anchor that operators grep for when triaging cost panels.
func TestCostMetrics_HelpStringsMentionOntology(t *testing.T) {
	r := NewRegistry()
	if err := Register(r); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mfs, err := r.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	wanted := map[string]bool{
		"weave_cost_storage_bytes_total":  false,
		"weave_cost_cpu_seconds_total":    false,
		"weave_cost_nats_messages_total":  false,
		"weave_cost_pg_rows":              false,
	}
	for _, mf := range mfs {
		if _, ok := wanted[mf.GetName()]; !ok {
			continue
		}
		if !strings.Contains(strings.ToLower(mf.GetHelp()), "ontology") {
			t.Errorf("help for %s does not mention 'ontology': %q", mf.GetName(), mf.GetHelp())
		}
		wanted[mf.GetName()] = true
	}
	for name, seen := range wanted {
		if !seen {
			t.Errorf("metric %s not gathered (cannot inspect help)", name)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
