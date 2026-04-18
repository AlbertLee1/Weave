package aggregation

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// setupHLLIndex creates a Bleve index with `cardinality` distinct values for
// the "tag" field spread across `total` total docs (each distinct value is
// repeated `total/cardinality` times). Used to exercise approximateDistinct
// at scale so the HLL path is engaged rather than the sparse / exact fallback.
func setupHLLIndex(t *testing.T, cardinality, total int) bleve.Index {
	t.Helper()
	if cardinality <= 0 || total < cardinality {
		t.Fatalf("invalid setup: cardinality=%d total=%d", cardinality, total)
	}
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("tag", mapping.NewKeywordFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "hll"), indexMapping)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	batch := idx.NewBatch()
	for i := 0; i < total; i++ {
		tag := fmt.Sprintf("tag-%d", i%cardinality)
		if err := batch.Index(fmt.Sprintf("doc-%d", i), map[string]interface{}{"tag": tag}); err != nil {
			t.Fatalf("batch index: %v", err)
		}
		if batch.Size() >= 500 {
			if err := idx.Batch(batch); err != nil {
				t.Fatalf("flush batch: %v", err)
			}
			batch = idx.NewBatch()
		}
	}
	if batch.Size() > 0 {
		if err := idx.Batch(batch); err != nil {
			t.Fatalf("final batch: %v", err)
		}
	}
	return idx
}

// TestApproximateDistinct_HighCardinalityError asserts that the HLL-backed
// estimator stays within 1% relative error on a high-cardinality dataset —
// this is the PRD acceptance criterion for US-228.
func TestApproximateDistinct_HighCardinalityError(t *testing.T) {
	const cardinality = 5000
	const total = 10000
	idx := setupHLLIndex(t, cardinality, total)

	eng := NewEngine()
	// Raise the scan cap above the dataset size so we count every doc.
	eng.MaxDocScanSize = total + 1
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "tag"},
		},
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
	err01 := math.Abs(float64(got-cardinality)) / float64(cardinality)
	if err01 > 0.01 {
		t.Errorf("approximateDistinct=%d, expected %d, relative error %.4f > 1%%", got, cardinality, err01)
	}
}

// TestApproximateDistinct_CustomPrecision exercises the Precision override
// path by requesting precision=16 (lower error than the default 14).
func TestApproximateDistinct_CustomPrecision(t *testing.T) {
	const cardinality = 5000
	const total = 10000
	idx := setupHLLIndex(t, cardinality, total)

	eng := NewEngine()
	eng.MaxDocScanSize = total + 1
	precision := 16
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "tag", Precision: &precision},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	val, ok := findMetric(resp.Data[0].Metrics, "tag.approximateDistinct")
	if !ok {
		t.Fatalf("expected metric key 'tag.approximateDistinct'")
	}
	got := val.(int)
	relErr := math.Abs(float64(got-cardinality)) / float64(cardinality)
	// Precision 16 standard error ~0.26%, allow 1% for scan variance.
	if relErr > 0.01 {
		t.Errorf("approximateDistinct (precision=16)=%d, expected %d, relative error %.4f > 1%%", got, cardinality, relErr)
	}
}

// TestApproximateDistinct_InvalidPrecision ensures the validator rejects
// precision values outside the HLL-legal range (4..18) with a clear error.
func TestApproximateDistinct_InvalidPrecision(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()
	cases := []int{0, 3, 19, 20}
	for _, p := range cases {
		precision := p
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{
				{Type: "approximateDistinct", Field: "department", Precision: &precision},
			},
		})
		if err == nil {
			t.Errorf("precision=%d: expected error, got nil", p)
		}
	}
}

// TestApproximateDistinct_EmptyIndex returns 0 (not nil) on an empty index so
// downstream JSON consumers don't have to handle a null-case.
func TestApproximateDistinct_EmptyIndex(t *testing.T) {
	idx := setupEmptyIndex(t)
	eng := NewEngine()
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	val, ok := findMetric(resp.Data[0].Metrics, "department.approximateDistinct")
	if !ok {
		t.Fatalf("expected metric key 'department.approximateDistinct'")
	}
	if val.(int) != 0 {
		t.Errorf("got approximateDistinct=%v on empty index, want 0", val)
	}
}

// TestApproximateDistinct_ExactOnSmallCardinality locks in the invariant that
// for small cardinalities HLL's sparse representation should return an EXACT
// count — the pre-HLL Bleve-facet implementation did the same, and existing
// callers rely on it.
func TestApproximateDistinct_ExactOnSmallCardinality(t *testing.T) {
	idx := setupAggIndex(t) // 3 distinct departments
	eng := NewEngine()
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	val, ok := findMetric(resp.Data[0].Metrics, "department.approximateDistinct")
	if !ok {
		t.Fatalf("expected metric key 'department.approximateDistinct'")
	}
	if val.(int) != 3 {
		t.Errorf("got approximateDistinct=%v, want 3 (sparse exact)", val)
	}
}
