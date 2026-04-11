package aggregation

import (
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// setupThreeLayerIndex builds an index with country (text) × price (numeric) ×
// orderDate (datetime) fields, suitable for 3-layer nested groupBy tests.
func setupThreeLayerIndex(t *testing.T) bleve.Index {
	t.Helper()
	idxMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("country", mapping.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("price", mapping.NewNumericFieldMapping())
	docMapping.AddFieldMappingsAt("orderDate", mapping.NewDateTimeFieldMapping())
	idxMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "three"), idxMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	// Spread across 2 countries × 2 price buckets (0-100, 100-200) × 2 90-day
	// windows (day 0, day 90+) so that all three layers actually split.
	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"1", map[string]interface{}{"country": "fra", "price": 50.0, "orderDate": "2024-01-10T00:00:00Z"}},
		{"2", map[string]interface{}{"country": "fra", "price": 150.0, "orderDate": "2024-01-20T00:00:00Z"}},
		{"3", map[string]interface{}{"country": "fra", "price": 160.0, "orderDate": "2024-05-05T00:00:00Z"}},
		{"4", map[string]interface{}{"country": "usa", "price": 30.0, "orderDate": "2024-01-15T00:00:00Z"}},
		{"5", map[string]interface{}{"country": "usa", "price": 40.0, "orderDate": "2024-01-25T00:00:00Z"}},
		{"6", map[string]interface{}{"country": "usa", "price": 120.0, "orderDate": "2024-05-01T00:00:00Z"}},
		{"7", map[string]interface{}{"country": "usa", "price": 130.0, "orderDate": "2024-05-10T00:00:00Z"}},
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("index doc %s: %v", d.id, err)
		}
	}
	return idx
}

// TestMultiGroupBy_ThreeLayerNested verifies that 3-layer nested groupBy
// (exact × fixedWidth × duration) produces a bucket tree where every row
// carries all three Group keys and counts match hand-computed values.
func TestMultiGroupBy_ThreeLayerNested(t *testing.T) {
	idx := setupThreeLayerIndex(t)
	eng := NewEngine()

	width := 100.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "country"},
			{Type: "fixedWidth", Field: "price", Width: &width},
			{Type: "duration", Field: "orderDate", DurationValue: &DurationValue{Unit: "DAYS", Value: 90}},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(resp.Data) == 0 {
		t.Fatalf("expected at least one row, got 0")
	}

	// Every row must carry all three group keys.
	for i, row := range resp.Data {
		if _, ok := row.Group["country"]; !ok {
			t.Errorf("row %d missing country key: %+v", i, row.Group)
		}
		if _, ok := row.Group["price"]; !ok {
			t.Errorf("row %d missing price key: %+v", i, row.Group)
		}
		if _, ok := row.Group["orderDate"]; !ok {
			t.Errorf("row %d missing orderDate key: %+v", i, row.Group)
		}
	}

	// Expected buckets (country, priceBucket, quarter-of-2024):
	//   fra / [0,100)   / Jan-early = 1   (doc 1)
	//   fra / [100,200) / Jan-early = 1   (doc 2)
	//   fra / [100,200) / Apr-ish   = 1   (doc 3)
	//   usa / [0,100)   / Jan-early = 2   (docs 4,5)
	//   usa / [100,200) / Apr-ish   = 2   (docs 6,7)
	if len(resp.Data) != 5 {
		t.Fatalf("expected 5 leaf rows, got %d: %+v", len(resp.Data), resp.Data)
	}

	// Sum of counts must equal total docs.
	var total uint64
	for _, row := range resp.Data {
		c, ok := findMetric(row.Metrics, "count")
		if !ok {
			t.Fatalf("row missing count metric: %+v", row)
		}
		total += c.(uint64)
	}
	if total != 7 {
		t.Errorf("leaf count sum = %d, want 7", total)
	}
}

// TestMultiGroupBy_StableBucketOrder verifies that buckets are ordered by
// groupBy declaration and, within each layer, by the group key ascending —
// regardless of facet backend ordering.
func TestMultiGroupBy_StableBucketOrder(t *testing.T) {
	idx := setupThreeLayerIndex(t)
	eng := NewEngine()

	width := 100.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "country"},
			{Type: "fixedWidth", Field: "price", Width: &width},
			{Type: "duration", Field: "orderDate", DurationValue: &DurationValue{Unit: "DAYS", Value: 90}},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	// Check outer-most country ordering: "fra" must appear before "usa".
	var countryOrder []string
	seenCountries := map[string]bool{}
	for _, row := range resp.Data {
		c, _ := row.Group["country"].(string)
		if !seenCountries[c] {
			seenCountries[c] = true
			countryOrder = append(countryOrder, c)
		}
	}
	if len(countryOrder) < 2 || countryOrder[0] != "fra" || countryOrder[1] != "usa" {
		t.Errorf("country order = %v, want [fra usa ...]", countryOrder)
	}

	// Within each country, price buckets must be ascending by lower bound.
	// And within each (country, price) pair, orderDate buckets must be ascending.
	type key struct {
		country string
		price   string
	}
	priceSeq := map[string][]string{}
	dateSeq := map[key][]string{}
	for _, row := range resp.Data {
		c, _ := row.Group["country"].(string)
		p, _ := row.Group["price"].(string)
		d, _ := row.Group["orderDate"].(string)
		priceSeq[c] = appendUnique(priceSeq[c], p)
		dateSeq[key{c, p}] = append(dateSeq[key{c, p}], d)
	}

	for country, seq := range priceSeq {
		if !isSortedAsc(seq) {
			t.Errorf("country %q price-bucket order not sorted asc: %v", country, seq)
		}
	}
	for k, seq := range dateSeq {
		if !isSortedAsc(seq) {
			t.Errorf("(%q, %q) orderDate order not sorted asc: %v", k.country, k.price, seq)
		}
	}
}

// TestMultiGroupBy_NullGroupKey verifies that documents missing the groupBy
// field surface in a bucket whose Group[field] is nil (JSON null), not the
// string "null".
func TestMultiGroupBy_NullGroupKey(t *testing.T) {
	t.Helper()
	idxMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("region", mapping.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("price", mapping.NewNumericFieldMapping())
	idxMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "null"), idxMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []map[string]interface{}{
		{"region": "apac", "price": 10.0},
		{"region": "emea", "price": 20.0},
		{"price": 30.0}, // missing region → null group
		{"price": 40.0}, // missing region → null group
	}
	for i, d := range docs {
		if err := idx.Index(string(rune('a'+i)), d); err != nil {
			t.Fatalf("index doc %d: %v", i, err)
		}
	}

	eng := NewEngine()
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "region"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	var nullRowCount int
	for _, row := range resp.Data {
		v, exists := row.Group["region"]
		if !exists {
			t.Errorf("row missing region key: %+v", row.Group)
			continue
		}
		if v == nil {
			nullRowCount++
			c, _ := findMetric(row.Metrics, "count")
			if got := c.(uint64); got != 2 {
				t.Errorf("null group count = %d, want 2", got)
			}
			continue
		}
		if s, ok := v.(string); ok && s == "null" {
			t.Errorf("null group rendered as string %q, want nil", s)
		}
	}
	if nullRowCount != 1 {
		t.Errorf("expected 1 null-group row, got %d; rows=%+v", nullRowCount, resp.Data)
	}
}

// isSortedAsc reports whether the input string slice is in ascending order.
func isSortedAsc(ss []string) bool {
	for i := 1; i < len(ss); i++ {
		if ss[i-1] > ss[i] {
			return false
		}
	}
	return true
}

// appendUnique appends s to ss only if ss does not already contain s.
func appendUnique(ss []string, s string) []string {
	for _, existing := range ss {
		if existing == s {
			return ss
		}
	}
	return append(ss, s)
}
