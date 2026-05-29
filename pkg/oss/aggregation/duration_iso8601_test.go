package aggregation

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// setupHourlyTimestampIndex builds a bleve index with three docs whose
// startDate epochs fall into two distinct 1-hour buckets: epoch 0 and 1800
// share [0,3600); epoch 7200 sits in [7200,10800).
func setupHourlyTimestampIndex(t *testing.T) bleve.Index {
	t.Helper()
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("startDate", mapping.NewNumericFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "hourly"), indexMapping)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"1", map[string]interface{}{"startDate": 0.0}},
		{"2", map[string]interface{}{"startDate": 1800.0}}, // same hour bucket as doc1
		{"3", map[string]interface{}{"startDate": 7200.0}}, // new hour bucket
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("failed to index doc %s: %v", d.id, err)
		}
	}
	return idx
}

// TestParseDuration_ISO8601 locks the full set of ISO 8601 duration shortcuts
// that Foundry's OntologyAggregation groupBy accepts (P1D / P1W / P1M / P3M /
// P1Y / PT1H — see docs/Palantir ObjectSet & OntologyAggregation 完整语法参考.md
// L563-572 + L856-862 .byQuarter()/.byHours()). P3M and PT1H were previously
// rejected by parseDuration, forcing callers to fall back to the verbose
// DurationValue{Unit,Value} form.
func TestParseDuration_ISO8601(t *testing.T) {
	cases := []struct {
		iso  string
		want time.Duration
	}{
		{"P1D", 24 * time.Hour},
		{"P1W", 7 * 24 * time.Hour},
		{"P1M", 30 * 24 * time.Hour},
		{"P3M", 90 * 24 * time.Hour}, // quarter == 3 × P1M, matching DurationValue{DAYS,90}
		{"P1Y", 365 * 24 * time.Hour},
		{"PT1H", time.Hour},
	}
	for _, tc := range cases {
		got, err := parseDuration(tc.iso)
		if err != nil {
			t.Errorf("parseDuration(%q) returned error: %v", tc.iso, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseDuration(%q) = %v, want %v", tc.iso, got, tc.want)
		}
	}

	// Unsupported shortcut still rejected.
	if _, err := parseDuration("P2W"); err == nil {
		t.Error("expected error for unsupported duration P2W, got nil")
	}
}

// TestAggregate_GroupByDuration_Quarter exercises the P3M (byQuarter) ISO 8601
// shortcut end-to-end through the bleve aggregation engine. With durSec = 90d,
// epoch buckets start at 0, 90d, 180d, ...
func TestAggregate_GroupByDuration_Quarter(t *testing.T) {
	idx := setupTimestampIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy: []GroupBySpec{
			{Type: "duration", Field: "startDate", Duration: "P3M"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate(P3M) returned error: %v", err)
	}

	// docs 1 (day 0) + 2 (day 10) + 3 (day 31) all fall in the first 90-day
	// bucket [0, 90d), so a single quarter bucket holds all three.
	if len(resp.Data) != 1 {
		t.Fatalf("got %d quarter buckets, want 1 (docs span only the first 90 days)", len(resp.Data))
	}
	count, ok := findMetric(resp.Data[0].Metrics, "count")
	if !ok {
		t.Fatalf("missing count metric for bucket %v", resp.Data[0].Group)
	}
	if count.(uint64) != 3 {
		t.Errorf("quarter bucket count = %d, want 3", count.(uint64))
	}
}

// TestAggregate_GroupByDuration_Hour exercises the PT1H (byHours) ISO 8601
// shortcut end-to-end. With durSec = 3600, hourly epoch buckets start at
// 0, 3600, 7200, ...
func TestAggregate_GroupByDuration_Hour(t *testing.T) {
	idx := setupHourlyTimestampIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy: []GroupBySpec{
			{Type: "duration", Field: "startDate", Duration: "PT1H"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate(PT1H) returned error: %v", err)
	}

	// 3 docs: epoch 0 + 1800 share hour bucket [0,3600); epoch 7200 is its own
	// bucket [7200,10800). Expect 2 hourly buckets.
	if len(resp.Data) != 2 {
		t.Fatalf("got %d hour buckets, want 2", len(resp.Data))
	}
	total := uint64(0)
	for _, row := range resp.Data {
		count, ok := findMetric(row.Metrics, "count")
		if !ok {
			t.Fatalf("missing count metric for bucket %v", row.Group)
		}
		total += count.(uint64)
	}
	if total != 3 {
		t.Errorf("total count across hour buckets = %d, want 3", total)
	}
}
