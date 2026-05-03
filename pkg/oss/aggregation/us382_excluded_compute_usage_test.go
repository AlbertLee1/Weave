package aggregation

import (
	"encoding/json"
	"testing"
)

// US-382: excludedItems pre-filters PKs before any aggregation runs, and the
// response carries a structured computeUsage envelope {scannedRows, durationMs,
// accuracy}. The fixture index seeded by setupAggIndex contains 5 docs
// (ids "1".."5") with three departments (engineering, sales, hr) and per-doc
// salaries.

func TestAggregate_ExcludedItems_FiltersBeforeCount(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	// Baseline: count over the full index = 5.
	baseline, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
	})
	if err != nil {
		t.Fatalf("baseline Aggregate: %v", err)
	}
	if got := baseline.Data[0].Metrics[0].Value.(uint64); got != 5 {
		t.Fatalf("baseline count: got %d want 5", got)
	}
	if baseline.ExcludedItems != 0 {
		t.Errorf("baseline excludedItems: got %d want 0", baseline.ExcludedItems)
	}

	// With 2 PKs excluded the count must drop to 3, and excludedItems must
	// report exactly 2 (the size of the intersection).
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations:  []AggregationSpec{{Type: "count"}},
		ExcludedItems: []string{"1", "2"},
	})
	if err != nil {
		t.Fatalf("Aggregate w/ exclusion: %v", err)
	}
	if got := resp.Data[0].Metrics[0].Value.(uint64); got != 3 {
		t.Errorf("post-exclusion count: got %d want 3", got)
	}
	if resp.ExcludedItems != 2 {
		t.Errorf("excludedItems: got %d want 2", resp.ExcludedItems)
	}
}

func TestAggregate_ExcludedItems_FiltersBeforeNumericMetrics(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	// Excluding "1" (salary 100k) and "2" (salary 120k) leaves
	// {sales:80k, sales:90k, hr:75k} with sum=245000 and avg ≈ 81666.67.
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "sum", Field: "salary"},
			{Type: "avg", Field: "salary"},
		},
		ExcludedItems: []string{"1", "2"},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	sum, _ := findMetric(resp.Data[0].Metrics, "salary.sum")
	if got := sum.(float64); got != 245000.0 {
		t.Errorf("sum: got %v want 245000.0", got)
	}
	avg, _ := findMetric(resp.Data[0].Metrics, "salary.avg")
	if got := avg.(float64); !floatApproxEqual(got, 81666.6667, 0.01) {
		t.Errorf("avg: got %v want ≈81666.67", got)
	}
	if resp.ExcludedItems != 2 {
		t.Errorf("excludedItems: got %d want 2", resp.ExcludedItems)
	}
}

func TestAggregate_ExcludedItems_FiltersBeforeGroupBy(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	// Exclude both engineering rows. Group-by department should now expose
	// only sales (2 rows) and hr (1 row).
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations:  []AggregationSpec{{Type: "count"}},
		GroupBy:       []GroupBySpec{{Type: "exact", Field: "department"}},
		ExcludedItems: []string{"1", "2"},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	if resp.ExcludedItems != 2 {
		t.Errorf("excludedItems: got %d want 2", resp.ExcludedItems)
	}

	wantBuckets := map[string]uint64{"sales": 2, "hr": 1}
	if len(resp.Data) != len(wantBuckets) {
		t.Fatalf("data length: got %d want %d (data=%+v)", len(resp.Data), len(wantBuckets), resp.Data)
	}
	for _, row := range resp.Data {
		dept, _ := row.Group["department"].(string)
		want, ok := wantBuckets[dept]
		if !ok {
			t.Errorf("unexpected bucket %q (engineering should be fully excluded)", dept)
			continue
		}
		if got := row.Metrics[0].Value.(uint64); got != want {
			t.Errorf("bucket %q: got count %d want %d", dept, got, want)
		}
	}
}

func TestAggregate_ExcludedItems_DeduplicatesAndIgnoresOutOfScope(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	// "1" appears twice, "" is blank, "999" doesn't exist in the index — only
	// "1" and "3" should contribute to excludedItems, so the count is 2.
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations:  []AggregationSpec{{Type: "count"}},
		ExcludedItems: []string{"1", "1", "", "999", "3"},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	if got := resp.Data[0].Metrics[0].Value.(uint64); got != 3 {
		t.Errorf("post-exclusion count: got %d want 3", got)
	}
	if resp.ExcludedItems != 2 {
		t.Errorf("excludedItems (dedup + out-of-scope): got %d want 2", resp.ExcludedItems)
	}
}

func TestAggregate_ExcludedItems_EmptyOrAllBlank_NoOp(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	for _, name := range []string{"empty", "all-blank"} {
		t.Run(name, func(t *testing.T) {
			req := &AggregationRequest{Aggregations: []AggregationSpec{{Type: "count"}}}
			if name == "all-blank" {
				req.ExcludedItems = []string{"", ""}
			}
			resp, err := eng.Aggregate(idx, req)
			if err != nil {
				t.Fatalf("Aggregate: %v", err)
			}
			if got := resp.Data[0].Metrics[0].Value.(uint64); got != 5 {
				t.Errorf("count: got %d want 5", got)
			}
			if resp.ExcludedItems != 0 {
				t.Errorf("excludedItems: got %d want 0", resp.ExcludedItems)
			}
		})
	}
}

func TestAggregate_ComputeUsage_StructureAndScannedRows(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	// No exclusion → scannedRows must equal the raw doc count (5).
	full, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if full.ComputeUsage == nil {
		t.Fatalf("ComputeUsage must be populated")
	}
	if full.ComputeUsage.ScannedRows != 5 {
		t.Errorf("ScannedRows: got %d want 5", full.ComputeUsage.ScannedRows)
	}
	if full.ComputeUsage.DurationMs < 0 {
		t.Errorf("DurationMs: must be non-negative, got %d", full.ComputeUsage.DurationMs)
	}
	if full.ComputeUsage.Accuracy != full.Accuracy {
		t.Errorf("ComputeUsage.Accuracy must mirror top-level: got %q want %q", full.ComputeUsage.Accuracy, full.Accuracy)
	}

	// With 2 excluded → scannedRows must reflect the post-filter total (3).
	excluded, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations:  []AggregationSpec{{Type: "count"}},
		ExcludedItems: []string{"1", "2"},
	})
	if err != nil {
		t.Fatalf("Aggregate w/ exclusion: %v", err)
	}
	if excluded.ComputeUsage == nil {
		t.Fatalf("ComputeUsage must be populated under exclusion")
	}
	if excluded.ComputeUsage.ScannedRows != 3 {
		t.Errorf("ScannedRows under exclusion: got %d want 3", excluded.ComputeUsage.ScannedRows)
	}
}

func TestAggregate_ComputeUsage_AccuracyMirrorsApproximate(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()
	// Force truncation of numeric scans by lowering MaxDocScanSize below the
	// fixture cardinality, then run a metric that requires document scan.
	eng.MaxDocScanSize = 2

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "sum", Field: "salary"}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if resp.Accuracy != "APPROXIMATE" {
		t.Fatalf("expected APPROXIMATE accuracy under MaxDocScanSize=2, got %q", resp.Accuracy)
	}
	if resp.ComputeUsage == nil || resp.ComputeUsage.Accuracy != "APPROXIMATE" {
		t.Errorf("ComputeUsage.Accuracy must mirror top-level APPROXIMATE, got %+v", resp.ComputeUsage)
	}
}

func TestAggregate_ComputeUsage_JSONShape(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations:  []AggregationSpec{{Type: "count"}},
		ExcludedItems: []string{"1"},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}

	wire, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded["excludedItems"].(float64) != 1 {
		t.Errorf("excludedItems in JSON: got %v want 1", decoded["excludedItems"])
	}
	cu, ok := decoded["computeUsage"].(map[string]interface{})
	if !ok {
		t.Fatalf("computeUsage must be an object, got %T (%v)", decoded["computeUsage"], decoded["computeUsage"])
	}
	for _, k := range []string{"scannedRows", "durationMs", "accuracy"} {
		if _, ok := cu[k]; !ok {
			t.Errorf("computeUsage.%s missing in JSON: %+v", k, cu)
		}
	}
	if cu["scannedRows"].(float64) != 4 {
		t.Errorf("computeUsage.scannedRows: got %v want 4", cu["scannedRows"])
	}
}
