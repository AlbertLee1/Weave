package aggregation

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// withMaxGroupByDepth temporarily lowers the package-level groupBy depth cap
// and restores it on cleanup. Tests rely on this to exercise the cap without
// having to build wide index mappings.
func withMaxGroupByDepth(t *testing.T, cap int) {
	t.Helper()
	prev := MaxGroupByDepth
	MaxGroupByDepth = cap
	t.Cleanup(func() { MaxGroupByDepth = prev })
}

// withMaxSubAggregationDepth temporarily lowers the package-level sub-agg
// depth cap and restores it on cleanup.
func withMaxSubAggregationDepth(t *testing.T, cap int) {
	t.Helper()
	prev := MaxSubAggregationDepth
	MaxSubAggregationDepth = cap
	t.Cleanup(func() { MaxSubAggregationDepth = prev })
}

// setupPercentileIndex indexes 1..n into a numeric "v" field. Ordered ingest
// is not required — percentile is order-invariant.
func setupPercentileIndex(t *testing.T, n int) bleve.Index {
	t.Helper()
	idxMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("v", mapping.NewNumericFieldMapping())
	idxMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "p"), idxMapping)
	if err != nil {
		t.Fatalf("bleve.New: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	batch := idx.NewBatch()
	for i := 1; i <= n; i++ {
		if err := batch.Index(fmt.Sprintf("d%d", i), map[string]interface{}{"v": float64(i)}); err != nil {
			t.Fatalf("batch.Index: %v", err)
		}
	}
	if err := idx.Batch(batch); err != nil {
		t.Fatalf("idx.Batch: %v", err)
	}
	return idx
}

// TestPercentile_Extremes_P0_P100_Empty pins percentile boundary semantics:
// p=0 returns the minimum, p=100 returns the maximum, and an empty index
// returns a nil metric value for both single- and multi-percentile specs,
// in both approximate (t-digest) and exact (sort) paths.
func TestPercentile_Extremes_P0_P100_Empty(t *testing.T) {
	const n = 50

	t.Run("Approx_P0_ReturnsMin", func(t *testing.T) {
		idx := setupPercentileIndex(t, n)
		eng := NewEngine()
		p := 0.0
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "approximatePercentile", Field: "v", Percentile: &p, Name: "p"}},
		})
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		got, ok := resp.Data[0].Metrics[0].Value.(float64)
		if !ok {
			t.Fatalf("metric value type = %T", resp.Data[0].Metrics[0].Value)
		}
		if math.Abs(got-1.0) > 1e-6 {
			t.Errorf("p0 = %v, want ≈ 1", got)
		}
	})

	t.Run("Approx_P100_ReturnsMax", func(t *testing.T) {
		idx := setupPercentileIndex(t, n)
		eng := NewEngine()
		p := 100.0
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "approximatePercentile", Field: "v", Percentile: &p, Name: "p"}},
		})
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		got, ok := resp.Data[0].Metrics[0].Value.(float64)
		if !ok {
			t.Fatalf("metric value type = %T", resp.Data[0].Metrics[0].Value)
		}
		if math.Abs(got-float64(n)) > 1e-6 {
			t.Errorf("p100 = %v, want ≈ %d", got, n)
		}
	})

	t.Run("Exact_P0_ReturnsMin", func(t *testing.T) {
		idx := setupPercentileIndex(t, n)
		eng := NewEngine()
		p := 0.0
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Accuracy:     AccuracyRequireAccurate,
			Aggregations: []AggregationSpec{{Type: "approximatePercentile", Field: "v", Percentile: &p, Name: "p"}},
		})
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		got, ok := resp.Data[0].Metrics[0].Value.(float64)
		if !ok {
			t.Fatalf("metric value type = %T", resp.Data[0].Metrics[0].Value)
		}
		if math.Abs(got-1.0) > 1e-9 {
			t.Errorf("exact p0 = %v, want 1", got)
		}
	})

	t.Run("Exact_P100_ReturnsMax", func(t *testing.T) {
		idx := setupPercentileIndex(t, n)
		eng := NewEngine()
		p := 100.0
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Accuracy:     AccuracyRequireAccurate,
			Aggregations: []AggregationSpec{{Type: "approximatePercentile", Field: "v", Percentile: &p, Name: "p"}},
		})
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		got, ok := resp.Data[0].Metrics[0].Value.(float64)
		if !ok {
			t.Fatalf("metric value type = %T", resp.Data[0].Metrics[0].Value)
		}
		if math.Abs(got-float64(n)) > 1e-9 {
			t.Errorf("exact p100 = %v, want %d", got, n)
		}
	})

	t.Run("Approx_EmptyIndex_Single_Nil", func(t *testing.T) {
		idx := setupEmptyIndex(t)
		eng := NewEngine()
		p := 50.0
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "approximatePercentile", Field: "salary", Percentile: &p, Name: "p"}},
		})
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		if resp.Data[0].Metrics[0].Value != nil {
			t.Errorf("empty single approx percentile value = %v, want nil", resp.Data[0].Metrics[0].Value)
		}
	})

	t.Run("Approx_EmptyIndex_Multi_Nil", func(t *testing.T) {
		idx := setupEmptyIndex(t)
		eng := NewEngine()
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{
				{Type: "approximatePercentile", Field: "salary", Percentiles: []float64{50, 99}, Name: "p"},
			},
		})
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		if resp.Data[0].Metrics[0].Value != nil {
			t.Errorf("empty multi approx percentile value = %v, want nil", resp.Data[0].Metrics[0].Value)
		}
	})

	t.Run("Exact_EmptyIndex_Single_Nil", func(t *testing.T) {
		idx := setupEmptyIndex(t)
		eng := NewEngine()
		p := 50.0
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Accuracy:     AccuracyRequireAccurate,
			Aggregations: []AggregationSpec{{Type: "approximatePercentile", Field: "salary", Percentile: &p, Name: "p"}},
		})
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		if resp.Data[0].Metrics[0].Value != nil {
			t.Errorf("empty single exact percentile value = %v, want nil", resp.Data[0].Metrics[0].Value)
		}
	})

	t.Run("Exact_EmptyIndex_Multi_Nil", func(t *testing.T) {
		idx := setupEmptyIndex(t)
		eng := NewEngine()
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Accuracy: AccuracyRequireAccurate,
			Aggregations: []AggregationSpec{
				{Type: "approximatePercentile", Field: "salary", Percentiles: []float64{50, 99}, Name: "p"},
			},
		})
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		if resp.Data[0].Metrics[0].Value != nil {
			t.Errorf("empty multi exact percentile value = %v, want nil", resp.Data[0].Metrics[0].Value)
		}
	})

	t.Run("MultiPercentile_P0_P100_Endpoints", func(t *testing.T) {
		idx := setupPercentileIndex(t, n)
		eng := NewEngine()
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Accuracy: AccuracyRequireAccurate,
			Aggregations: []AggregationSpec{
				{Type: "approximatePercentile", Field: "v", Percentiles: []float64{0, 100}, Name: "p"},
			},
		})
		if err != nil {
			t.Fatalf("Aggregate: %v", err)
		}
		got, ok := resp.Data[0].Metrics[0].Value.(map[string]float64)
		if !ok {
			t.Fatalf("metric value type = %T, want map[string]float64", resp.Data[0].Metrics[0].Value)
		}
		if v := got["0"]; math.Abs(v-1.0) > 1e-9 {
			t.Errorf("multi p0 = %v, want 1", v)
		}
		if v := got["100"]; math.Abs(v-float64(n)) > 1e-9 {
			t.Errorf("multi p100 = %v, want %d", v, n)
		}
	})
}

// TestComputeMetrics_UnsupportedTypeErrors guards against silent metric drops
// when a caller specifies an unknown aggregation type. The engine must surface
// the typo as an error rather than returning an empty metric set.
func TestComputeMetrics_UnsupportedTypeErrors(t *testing.T) {
	t.Run("UnknownType", func(t *testing.T) {
		idx := setupAggIndex(t)
		eng := NewEngine()
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "median", Field: "salary"}},
		})
		if err == nil {
			t.Fatalf("expected error for unknown aggregation type")
		}
		if !strings.Contains(err.Error(), "median") {
			t.Errorf("error %q does not mention bad type", err.Error())
		}
	})

	t.Run("EmptyType", func(t *testing.T) {
		idx := setupAggIndex(t)
		eng := NewEngine()
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Field: "salary"}}, // Type ""
		})
		if err == nil {
			t.Fatalf("expected error for empty aggregation type")
		}
	})

	t.Run("InSubAggregation", func(t *testing.T) {
		idx := setupAggIndex(t)
		eng := NewEngine()
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "count", Name: "all"}},
			SubAggregations: []SubAggregationSpec{
				{Name: "bad", Aggregations: []AggregationSpec{{Type: "nope", Field: "salary"}}},
			},
		})
		if err == nil {
			t.Fatalf("expected error for unknown aggregation type in sub-aggregation")
		}
	})
}

// TestGroupBy_DepthLimit verifies that a single request honours a hard cap on
// the number of declared groupBy layers. The test temporarily lowers the
// cap to 2 (vs. the default 8) so we can exercise both at-cap and over-cap
// paths cheaply.
func TestGroupBy_DepthLimit(t *testing.T) {
	t.Run("AtCapAccepted", func(t *testing.T) {
		withMaxGroupByDepth(t, 2)
		idx := setupAggIndex(t)
		eng := NewEngine()
		width := 50000.0
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "count"}},
			GroupBy: []GroupBySpec{
				{Type: "exact", Field: "department"},
				{Type: "fixedWidth", Field: "salary", Width: &width},
			},
		})
		if err != nil {
			t.Fatalf("at-cap request rejected: %v", err)
		}
	})

	t.Run("OverCapRejected", func(t *testing.T) {
		withMaxGroupByDepth(t, 2)
		idx := setupAggIndex(t)
		eng := NewEngine()
		width := 50000.0
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "count"}},
			GroupBy: []GroupBySpec{
				{Type: "exact", Field: "department"},
				{Type: "fixedWidth", Field: "salary", Width: &width},
				{Type: "exact", Field: "active"}, // 3rd layer over a 2-deep cap
			},
		})
		if err == nil {
			t.Fatalf("expected error for over-cap groupBy depth")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "groupby") {
			t.Errorf("error %q does not mention groupBy depth", err.Error())
		}
	})

	t.Run("LimitAppliesToSubAggregations", func(t *testing.T) {
		// Sub-aggregations recurse via AggregateWithQuery, so the cap applies
		// per-request inside each child too. A 1-deep cap should reject a
		// sub-aggregation that declares 2 groupBy layers.
		withMaxGroupByDepth(t, 1)
		idx := setupAggIndex(t)
		eng := NewEngine()
		width := 50000.0
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "count", Name: "all"}},
			SubAggregations: []SubAggregationSpec{
				{
					Name:         "byTwo",
					Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
					GroupBy: []GroupBySpec{
						{Type: "exact", Field: "department"},
						{Type: "fixedWidth", Field: "salary", Width: &width},
					},
				},
			},
		})
		if err == nil {
			t.Fatalf("expected error: sub-aggregation groupBy exceeds cap")
		}
	})
}

// TestSubAggregation_DepthLimit verifies the recursive sub-aggregation cap so
// a runaway spec can't blow the heap. The cap is on tree depth, not breadth.
func TestSubAggregation_DepthLimit(t *testing.T) {
	t.Run("AtCapAccepted", func(t *testing.T) {
		withMaxSubAggregationDepth(t, 2)
		idx := setupSubAggIndex(t)
		eng := NewEngine()
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "count", Name: "all"}},
			SubAggregations: []SubAggregationSpec{
				{
					Name:         "outer",
					Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
					SubAggregations: []SubAggregationSpec{
						{
							Name:         "inner",
							Aggregations: []AggregationSpec{{Type: "count", Name: "m"}},
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("at-cap sub-aggregation rejected: %v", err)
		}
	})

	t.Run("OverCapRejected", func(t *testing.T) {
		withMaxSubAggregationDepth(t, 2)
		idx := setupSubAggIndex(t)
		eng := NewEngine()
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "count", Name: "all"}},
			SubAggregations: []SubAggregationSpec{
				{
					Name:         "L1",
					Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
					SubAggregations: []SubAggregationSpec{
						{
							Name:         "L2",
							Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
							SubAggregations: []SubAggregationSpec{
								{
									Name:         "L3",
									Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
								},
							},
						},
					},
				},
			},
		})
		if err == nil {
			t.Fatalf("expected error for over-cap sub-aggregation depth")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "subaggregation") &&
			!strings.Contains(strings.ToLower(err.Error()), "depth") {
			t.Errorf("error %q does not mention depth", err.Error())
		}
	})
}

// TestApproximateDistinct_LargeCardinality_ErrorBound extends the existing
// 5k-cardinality HLL gate to 30k unique values, asserting the same ≤1%
// relative-error contract holds at the default precision (14). Skipped in
// -short to keep make test-unit snappy.
func TestApproximateDistinct_LargeCardinality_ErrorBound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-cardinality HLL accuracy test in -short mode")
	}
	const cardinality = 30000
	const total = 30000
	idx := setupHLLIndex(t, cardinality, total)

	eng := NewEngine()
	eng.MaxDocScanSize = total + 1

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "approximateDistinct", Field: "tag"}},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	val, ok := findMetric(resp.Data[0].Metrics, "tag.approximateDistinct")
	if !ok {
		t.Fatalf("expected metric key 'tag.approximateDistinct'")
	}
	got, ok := val.(int)
	if !ok {
		t.Fatalf("expected int result, got %T (%v)", val, val)
	}
	rel := math.Abs(float64(got-cardinality)) / float64(cardinality)
	if rel > 0.01 {
		t.Errorf("approximateDistinct=%d on %d unique values, relative error %.4f > 1%%", got, cardinality, rel)
	}
}
