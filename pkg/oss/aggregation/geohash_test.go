package aggregation

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/mmcloughlin/geohash"
)

// setupGeoIndex creates a Bleve index with a geopoint field and a few
// distinct clusters of points so geohash groupings are predictable.
//
// Clusters (each has 2 points within ~100m of its centre):
//
//	SF:  37.7749, -122.4194
//	NYC: 40.7128,  -74.0060
//	TYO: 35.6762,  139.6503
func setupGeoIndex(t *testing.T) bleve.Index {
	t.Helper()
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("location", mapping.NewGeoPointFieldMapping())
	docMapping.AddFieldMappingsAt("label", mapping.NewTextFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "geo"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"sf1", map[string]interface{}{"location": []interface{}{-122.4194, 37.7749}, "label": "sf"}},
		{"sf2", map[string]interface{}{"location": []interface{}{-122.4200, 37.7750}, "label": "sf"}},
		{"nyc1", map[string]interface{}{"location": []interface{}{-74.0060, 40.7128}, "label": "nyc"}},
		{"nyc2", map[string]interface{}{"location": []interface{}{-74.0050, 40.7130}, "label": "nyc"}},
		{"tyo1", map[string]interface{}{"location": []interface{}{139.6503, 35.6762}, "label": "tyo"}},
		{"tyo2", map[string]interface{}{"location": map[string]interface{}{"lon": 139.6500, "lat": 35.6760}, "label": "tyo"}},
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("index %s: %v", d.id, err)
		}
	}
	return idx
}

func TestGeohash_GroupBy_DefaultPrecision(t *testing.T) {
	idx := setupGeoIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy:      []GroupBySpec{{Type: "geohash", Field: "location"}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	// Default precision 6 keeps each city's pair of points in ONE cell; different
	// cities sit in different cells, so we expect 3 rows with count=2 each.
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 rows, got %d: %#v", len(resp.Data), resp.Data)
	}
	for _, row := range resp.Data {
		count, ok := findMetric(row.Metrics, "count")
		if !ok {
			t.Fatalf("row missing count metric: %#v", row)
		}
		if count.(uint64) != 2 {
			t.Errorf("row %v: got count %v, want 2", row.Group["location"], count)
		}
		hash, ok := row.Group["location"].(string)
		if !ok || len(hash) != 6 {
			t.Errorf("group value expected 6-char geohash string, got %T %v", row.Group["location"], row.Group["location"])
		}
	}
}

func TestGeohash_GroupBy_CustomPrecision(t *testing.T) {
	idx := setupGeoIndex(t)
	eng := NewEngine()

	precision := 1
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy:      []GroupBySpec{{Type: "geohash", Field: "location", Precision: &precision}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	// Precision 1 (≈5000km cells) MAY merge the SF + NYC clusters if they
	// fall in the same base32 cell, and the TYO cluster is on the far side
	// of the globe in its own cell. We assert at-most 3 cells and that SUM
	// of counts still equals 6.
	if len(resp.Data) < 1 || len(resp.Data) > 3 {
		t.Fatalf("expected 1..3 rows at precision 1, got %d: %#v", len(resp.Data), resp.Data)
	}
	var sum uint64
	for _, row := range resp.Data {
		c, _ := findMetric(row.Metrics, "count")
		sum += c.(uint64)
		hash, ok := row.Group["location"].(string)
		if !ok || len(hash) != 1 {
			t.Errorf("row %#v: expected 1-char hash, got %v (%T)", row, row.Group["location"], row.Group["location"])
		}
	}
	if sum != 6 {
		t.Errorf("sum of counts = %d, want 6", sum)
	}
}

func TestGeohash_GroupBy_HashMatchesLibraryOutput(t *testing.T) {
	idx := setupGeoIndex(t)
	eng := NewEngine()

	precision := 6
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy:      []GroupBySpec{{Type: "geohash", Field: "location", Precision: &precision}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	// Compute the expected geohash for each centre point directly and check
	// at least one of the result rows matches.
	expected := map[string]bool{
		geohash.EncodeWithPrecision(37.7749, -122.4194, 6): false,
		geohash.EncodeWithPrecision(40.7128, -74.0060, 6):  false,
		geohash.EncodeWithPrecision(35.6762, 139.6503, 6):  false,
	}
	for _, row := range resp.Data {
		h := row.Group["location"].(string)
		if _, ok := expected[h]; ok {
			expected[h] = true
		}
	}
	for hash, seen := range expected {
		if !seen {
			t.Errorf("expected hash %q missing from response rows", hash)
		}
	}
}

func TestGeohash_GroupBy_PrecisionOutOfRange(t *testing.T) {
	idx := setupGeoIndex(t)
	eng := NewEngine()

	for _, p := range []int{0, -1, 13, 15, 100} {
		pCopy := p
		_, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{{Type: "count"}},
			GroupBy:      []GroupBySpec{{Type: "geohash", Field: "location", Precision: &pCopy}},
		})
		if err == nil {
			t.Errorf("precision %d: expected error, got nil", p)
		}
	}
}

func TestGeohash_GroupBy_EmptyIndex(t *testing.T) {
	idx := setupEmptyIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy:      []GroupBySpec{{Type: "geohash", Field: "location"}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Errorf("expected 0 rows on empty index, got %d: %#v", len(resp.Data), resp.Data)
	}
}

func TestGeohash_GroupBy_MissingField(t *testing.T) {
	// Docs with no geopoint field: groupBy should produce no buckets (the
	// hits won't carry a decodable value; the walker skips them).
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("label", mapping.NewTextFieldMapping())
	indexMapping.DefaultMapping = docMapping
	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "nogeo"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	defer idx.Close()

	if err := idx.Index("a", map[string]interface{}{"label": "x"}); err != nil {
		t.Fatalf("index: %v", err)
	}

	eng := NewEngine()
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy:      []GroupBySpec{{Type: "geohash", Field: "location"}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Errorf("expected 0 rows for docs with no location field, got %d", len(resp.Data))
	}
}

func TestGeohash_GroupBy_NoField(t *testing.T) {
	idx := setupGeoIndex(t)
	eng := NewEngine()

	_, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy:      []GroupBySpec{{Type: "geohash"}},
	})
	if err == nil {
		t.Fatal("expected error for missing Field, got nil")
	}
}

func TestGeohash_GroupBy_PerCityMetrics(t *testing.T) {
	idx := setupGeoIndex(t)
	eng := NewEngine()

	precision := 6
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
			{Type: "approximateDistinct", Field: "label", Name: "labels"},
		},
		GroupBy: []GroupBySpec{{Type: "geohash", Field: "location", Precision: &precision}},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	// At precision 6 each cluster is one cell. labels should be 1 per cell.
	if len(resp.Data) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(resp.Data))
	}
	for _, row := range resp.Data {
		labels, _ := findMetric(row.Metrics, "labels")
		if labels.(int) != 1 {
			t.Errorf("row %v: expected 1 distinct label, got %v", row.Group["location"], labels)
		}
	}
}

func TestGeohash_GroupBy_DeterministicOrder(t *testing.T) {
	idx := setupGeoIndex(t)
	eng := NewEngine()

	req := &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy:      []GroupBySpec{{Type: "geohash", Field: "location"}},
	}
	resp1, _ := eng.Aggregate(idx, req)
	resp2, _ := eng.Aggregate(idx, req)

	keys := func(rows []AggregationRow) []string {
		out := make([]string, 0, len(rows))
		for _, r := range rows {
			out = append(out, r.Group["location"].(string))
		}
		return out
	}
	k1 := keys(resp1.Data)
	k2 := keys(resp2.Data)
	// Order is imposed by sortGroupEntries → alphabetical on the hash string.
	sorted := append([]string(nil), k1...)
	sort.Strings(sorted)
	for i := range k1 {
		if k1[i] != sorted[i] {
			t.Errorf("row %d: got %q, want sorted %q (full=%v)", i, k1[i], sorted[i], k1)
		}
		if k1[i] != k2[i] {
			t.Errorf("order not stable across runs: %v vs %v", k1, k2)
		}
	}
}

// --- Decoder unit tests (shape tolerance) ---

func TestDecodeGeopoint_FloatSliceLonLat(t *testing.T) {
	lat, lng, ok := decodeGeopoint([]float64{-122.4194, 37.7749})
	if !ok {
		t.Fatal("expected ok")
	}
	if lat != 37.7749 || lng != -122.4194 {
		t.Errorf("got lat=%v lng=%v", lat, lng)
	}
}

func TestDecodeGeopoint_InterfaceSliceLonLat(t *testing.T) {
	lat, lng, ok := decodeGeopoint([]interface{}{-74.006, 40.7128})
	if !ok {
		t.Fatal("expected ok")
	}
	if lat != 40.7128 || lng != -74.006 {
		t.Errorf("got lat=%v lng=%v", lat, lng)
	}
}

func TestDecodeGeopoint_MapForm(t *testing.T) {
	// lat/lon
	lat, lng, ok := decodeGeopoint(map[string]interface{}{"lat": 1.0, "lon": 2.0})
	if !ok || lat != 1.0 || lng != 2.0 {
		t.Errorf("lat/lon: ok=%v lat=%v lng=%v", ok, lat, lng)
	}
	// lat/lng
	lat, lng, ok = decodeGeopoint(map[string]interface{}{"lat": 3.0, "lng": 4.0})
	if !ok || lat != 3.0 || lng != 4.0 {
		t.Errorf("lat/lng: ok=%v lat=%v lng=%v", ok, lat, lng)
	}
	// latitude/longitude
	lat, lng, ok = decodeGeopoint(map[string]interface{}{"latitude": 5.0, "longitude": 6.0})
	if !ok || lat != 5.0 || lng != 6.0 {
		t.Errorf("latitude/longitude: ok=%v lat=%v lng=%v", ok, lat, lng)
	}
}

func TestDecodeGeopoint_InvalidShapes(t *testing.T) {
	cases := []interface{}{
		nil,
		"37.7749,-122.4194", // string form not supported
		[]float64{1.0},      // too short
		[]interface{}{"a", "b"},
		map[string]interface{}{}, // missing keys
		map[string]interface{}{"lat": "oops"},
		42,
	}
	for _, c := range cases {
		if _, _, ok := decodeGeopoint(c); ok {
			t.Errorf("expected decode fail for %#v", c)
		}
	}
}
