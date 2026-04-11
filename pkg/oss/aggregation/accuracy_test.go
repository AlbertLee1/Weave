package aggregation

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// setupAccuracyIndex builds a small index whose doc count can be tuned so
// the scan-size truncation path is deterministic to hit.
func setupAccuracyIndex(t *testing.T, n int) bleve.Index {
	t.Helper()
	idxMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("price", mapping.NewNumericFieldMapping())
	docMapping.AddFieldMappingsAt("region", mapping.NewTextFieldMapping())
	idxMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "accuracy"), idxMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	for i := 0; i < n; i++ {
		doc := map[string]interface{}{
			"price":  float64(i) + 1.0,
			"region": fmt.Sprintf("r%d", i%3),
		}
		if err := idx.Index(fmt.Sprintf("doc-%d", i), doc); err != nil {
			t.Fatalf("index doc %d: %v", i, err)
		}
	}
	return idx
}

// TestAggregationAccuracyMarker verifies that the aggregation response's
// top-level `accuracy` field is "ACCURATE" when the scan covers every
// matching document, and "APPROXIMATE" when the MaxDocScanSize ceiling
// truncates the scan — regardless of whether groupBy is used.
func TestAggregationAccuracyMarker(t *testing.T) {
	t.Run("simple avg, scan fits all docs -> ACCURATE", func(t *testing.T) {
		idx := setupAccuracyIndex(t, 10)
		eng := NewEngine()
		eng.MaxDocScanSize = 100

		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{
				{Type: "avg", Field: "price", Name: "avgPrice"},
			},
		})
		if err != nil {
			t.Fatalf("Aggregate error: %v", err)
		}
		if resp.Accuracy != "ACCURATE" {
			t.Errorf("accuracy = %q, want ACCURATE", resp.Accuracy)
		}
	})

	t.Run("simple avg, MaxDocScanSize < total -> APPROXIMATE", func(t *testing.T) {
		idx := setupAccuracyIndex(t, 20)
		eng := NewEngine()
		eng.MaxDocScanSize = 5

		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{
				{Type: "avg", Field: "price", Name: "avgPrice"},
			},
		})
		if err != nil {
			t.Fatalf("Aggregate error: %v", err)
		}
		if resp.Accuracy != "APPROXIMATE" {
			t.Errorf("accuracy = %q, want APPROXIMATE", resp.Accuracy)
		}
	})

	t.Run("standardDeviation scan truncated -> APPROXIMATE", func(t *testing.T) {
		idx := setupAccuracyIndex(t, 30)
		eng := NewEngine()
		eng.MaxDocScanSize = 10

		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{
				{Type: "standardDeviation", Field: "price", Name: "sdPrice"},
			},
		})
		if err != nil {
			t.Fatalf("Aggregate error: %v", err)
		}
		if resp.Accuracy != "APPROXIMATE" {
			t.Errorf("accuracy = %q, want APPROXIMATE", resp.Accuracy)
		}
	})

	t.Run("approximatePercentile scan truncated -> APPROXIMATE", func(t *testing.T) {
		idx := setupAccuracyIndex(t, 50)
		eng := NewEngine()
		eng.MaxDocScanSize = 8

		p := 90.0
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{
				{Type: "approximatePercentile", Field: "price", Percentile: &p, Name: "p90"},
			},
		})
		if err != nil {
			t.Fatalf("Aggregate error: %v", err)
		}
		if resp.Accuracy != "APPROXIMATE" {
			t.Errorf("accuracy = %q, want APPROXIMATE", resp.Accuracy)
		}
	})

	t.Run("groupBy exact + truncated leaf sum -> APPROXIMATE", func(t *testing.T) {
		idx := setupAccuracyIndex(t, 30)
		eng := NewEngine()
		eng.MaxDocScanSize = 4

		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{
				{Type: "sum", Field: "price", Name: "sumPrice"},
			},
			GroupBy: []GroupBySpec{
				{Type: "exact", Field: "region"},
			},
		})
		if err != nil {
			t.Fatalf("Aggregate error: %v", err)
		}
		if resp.Accuracy != "APPROXIMATE" {
			t.Errorf("accuracy = %q, want APPROXIMATE", resp.Accuracy)
		}
	})

	t.Run("count-only query ignores scan size -> ACCURATE", func(t *testing.T) {
		idx := setupAccuracyIndex(t, 100)
		eng := NewEngine()
		eng.MaxDocScanSize = 3

		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{
				{Type: "count"},
			},
		})
		if err != nil {
			t.Fatalf("Aggregate error: %v", err)
		}
		if resp.Accuracy != "ACCURATE" {
			t.Errorf("accuracy = %q, want ACCURATE (count uses facet total, not scan)", resp.Accuracy)
		}
	})
}
