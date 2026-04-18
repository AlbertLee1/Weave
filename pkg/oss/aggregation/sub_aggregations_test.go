package aggregation

import (
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// setupSubAggIndex builds an index with country (text) × price (numeric)
// fields used by sub-aggregation tests.
func setupSubAggIndex(t *testing.T) bleve.Index {
	t.Helper()
	idxMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("country", mapping.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("price", mapping.NewNumericFieldMapping())
	idxMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "subagg"), idxMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"1", map[string]interface{}{"country": "fra", "price": 10.0}},
		{"2", map[string]interface{}{"country": "fra", "price": 20.0}},
		{"3", map[string]interface{}{"country": "fra", "price": 150.0}},
		{"4", map[string]interface{}{"country": "usa", "price": 30.0}},
		{"5", map[string]interface{}{"country": "usa", "price": 40.0}},
		{"6", map[string]interface{}{"country": "usa", "price": 200.0}},
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("index doc %s: %v", d.id, err)
		}
	}
	return idx
}

// TestSubAggregations_PerBucket verifies that a sub-aggregation runs with the
// scope of each top-level group bucket (different countries get different
// bucket distributions of the inner fixed-width price aggregation).
func TestSubAggregations_PerBucket(t *testing.T) {
	idx := setupSubAggIndex(t)
	eng := NewEngine()

	width := 100.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "totalCount"}},
		GroupBy:      []GroupBySpec{{Type: "exact", Field: "country"}},
		SubAggregations: []SubAggregationSpec{
			{
				Name:         "byPriceBucket",
				Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
				GroupBy: []GroupBySpec{
					{Type: "fixedWidth", Field: "price", Width: &width},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 country rows, got %d", len(resp.Data))
	}

	for _, row := range resp.Data {
		if row.SubAggregations == nil {
			t.Fatalf("row %v missing SubAggregations", row.Group)
		}
		sub, ok := row.SubAggregations["byPriceBucket"]
		if !ok {
			t.Fatalf("row %v missing 'byPriceBucket' sub-aggregation result", row.Group)
		}
		if len(sub.Data) == 0 {
			t.Errorf("row %v: empty sub-aggregation buckets", row.Group)
		}

		// Sum of inner counts must equal outer total for that country.
		var innerSum uint64
		for _, sr := range sub.Data {
			n, ok := findMetric(sr.Metrics, "n")
			if !ok {
				t.Errorf("inner row missing 'n' metric: %+v", sr)
				continue
			}
			innerSum += n.(uint64)
		}
		outerCount, _ := findMetric(row.Metrics, "totalCount")
		if outerCount.(uint64) != innerSum {
			t.Errorf("country %v: outer count=%d, inner sum=%d", row.Group["country"], outerCount, innerSum)
		}
	}
}

// TestSubAggregations_TopLevel verifies sub-aggregations work even when the
// outer request has no groupBy — they attach to the response itself.
func TestSubAggregations_TopLevel(t *testing.T) {
	idx := setupSubAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "all"}},
		SubAggregations: []SubAggregationSpec{
			{
				Name:         "byCountry",
				Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
				GroupBy:      []GroupBySpec{{Type: "exact", Field: "country"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if resp.SubAggregations == nil {
		t.Fatalf("top-level SubAggregations missing")
	}
	sub, ok := resp.SubAggregations["byCountry"]
	if !ok {
		t.Fatalf("missing 'byCountry' sub-aggregation in top-level response")
	}
	if len(sub.Data) != 2 {
		t.Errorf("expected 2 country buckets, got %d", len(sub.Data))
	}
	var innerSum uint64
	for _, row := range sub.Data {
		n, _ := findMetric(row.Metrics, "n")
		innerSum += n.(uint64)
	}
	if innerSum != 6 {
		t.Errorf("sum of inner counts = %d, want 6", innerSum)
	}
}

// TestSubAggregations_NestedTwoDeep verifies sub-aggregations themselves can
// carry sub-aggregations (depth > 1).
func TestSubAggregations_NestedTwoDeep(t *testing.T) {
	idx := setupSubAggIndex(t)
	eng := NewEngine()

	width := 100.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "all"}},
		SubAggregations: []SubAggregationSpec{
			{
				Name:         "byCountry",
				Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
				GroupBy:      []GroupBySpec{{Type: "exact", Field: "country"}},
				SubAggregations: []SubAggregationSpec{
					{
						Name:         "byPrice",
						Aggregations: []AggregationSpec{{Type: "count", Name: "p"}},
						GroupBy:      []GroupBySpec{{Type: "fixedWidth", Field: "price", Width: &width}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	outer, ok := resp.SubAggregations["byCountry"]
	if !ok {
		t.Fatalf("missing top-level byCountry")
	}
	for _, row := range outer.Data {
		nested, ok := row.SubAggregations["byPrice"]
		if !ok {
			t.Errorf("country %v: missing byPrice nested sub-aggregation", row.Group)
			continue
		}
		if len(nested.Data) == 0 {
			t.Errorf("country %v: empty nested data", row.Group)
		}
	}
}

// TestSubAggregations_ValidateMissingName rejects a sub-aggregation without a
// Name since results would be unaddressable.
func TestSubAggregations_ValidateMissingName(t *testing.T) {
	idx := setupSubAggIndex(t)
	eng := NewEngine()

	_, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "all"}},
		SubAggregations: []SubAggregationSpec{
			{
				Aggregations: []AggregationSpec{{Type: "count"}},
			},
		},
	})
	if err == nil {
		t.Fatalf("expected validation error for missing sub-aggregation name")
	}
}

// TestSubAggregations_ValidateDuplicateName rejects two sub-aggregations
// sharing the same name.
func TestSubAggregations_ValidateDuplicateName(t *testing.T) {
	idx := setupSubAggIndex(t)
	eng := NewEngine()

	_, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "all"}},
		SubAggregations: []SubAggregationSpec{
			{Name: "x", Aggregations: []AggregationSpec{{Type: "count"}}},
			{Name: "x", Aggregations: []AggregationSpec{{Type: "count"}}},
		},
	})
	if err == nil {
		t.Fatalf("expected validation error for duplicate sub-aggregation name")
	}
}
