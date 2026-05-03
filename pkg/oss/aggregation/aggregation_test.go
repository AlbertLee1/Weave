package aggregation

import (
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// findMetric is a test helper that looks up a MetricValue by name from a slice.
func findMetric(metrics []MetricValue, name string) (interface{}, bool) {
	for _, m := range metrics {
		if m.Name == name {
			return m.Value, true
		}
	}
	return nil, false
}

// setupAggIndex creates a Bleve index with test data for aggregation tests.
func setupAggIndex(t *testing.T) bleve.Index {
	t.Helper()
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("department", mapping.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("salary", mapping.NewNumericFieldMapping())
	docMapping.AddFieldMappingsAt("age", mapping.NewNumericFieldMapping())
	docMapping.AddFieldMappingsAt("active", mapping.NewBooleanFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "agg"), indexMapping)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"1", map[string]interface{}{"department": "engineering", "salary": 100000.0, "age": 30.0, "active": true}},
		{"2", map[string]interface{}{"department": "engineering", "salary": 120000.0, "age": 35.0, "active": true}},
		{"3", map[string]interface{}{"department": "sales", "salary": 80000.0, "age": 28.0, "active": true}},
		{"4", map[string]interface{}{"department": "sales", "salary": 90000.0, "age": 32.0, "active": false}},
		{"5", map[string]interface{}{"department": "hr", "salary": 75000.0, "age": 40.0, "active": true}},
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("failed to index doc %s: %v", d.id, err)
		}
	}
	return idx
}

// setupEmptyIndex creates an empty Bleve index.
func setupEmptyIndex(t *testing.T) bleve.Index {
	t.Helper()
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("department", mapping.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("salary", mapping.NewNumericFieldMapping())
	docMapping.AddFieldMappingsAt("age", mapping.NewNumericFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "empty"), indexMapping)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })
	return idx
}

// --- AggregationSpec / Request type tests (1-3) ---

func TestAggregationSpec_Count(t *testing.T) {
	spec := AggregationSpec{Type: "count"}
	if spec.Type != "count" {
		t.Errorf("got type %q, want %q", spec.Type, "count")
	}
	if spec.Field != "" {
		t.Errorf("got field %q, want empty", spec.Field)
	}
}

func TestAggregationSpec_WithName(t *testing.T) {
	spec := AggregationSpec{Type: "avg", Field: "salary", Name: "avgSalary"}
	if spec.Name != "avgSalary" {
		t.Errorf("got name %q, want %q", spec.Name, "avgSalary")
	}
	// Verify the name is used in metric output.
	idx := setupAggIndex(t)
	eng := NewEngine()
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{spec},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	if _, ok := findMetric(resp.Data[0].Metrics, "avgSalary"); !ok {
		t.Errorf("expected metric key %q, got metrics: %v", "avgSalary", resp.Data[0].Metrics)
	}
}

func TestGroupBySpec_Exact(t *testing.T) {
	spec := GroupBySpec{Type: "exact", Field: "department"}
	if spec.Type != "exact" {
		t.Errorf("got type %q, want %q", spec.Type, "exact")
	}
	if spec.Field != "department" {
		t.Errorf("got field %q, want %q", spec.Field, "department")
	}
}

// --- Simple aggregation - count (4-5) ---

func TestAggregate_Count(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("got %d rows, want 1", len(resp.Data))
	}

	count, ok := findMetric(resp.Data[0].Metrics, "count")
	if !ok {
		t.Fatal("expected metric key 'count'")
	}
	if count.(uint64) != 5 {
		t.Errorf("got count %v, want 5", count)
	}
}

func TestAggregate_CountEmpty(t *testing.T) {
	idx := setupEmptyIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	count, ok := findMetric(resp.Data[0].Metrics, "count")
	if !ok {
		t.Fatal("expected metric key 'count'")
	}
	if count.(uint64) != 0 {
		t.Errorf("got count %v, want 0", count)
	}
}

// --- Numeric aggregations (6-10) ---

func TestAggregate_Min(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "min", Field: "salary"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "salary.min")
	if !ok {
		t.Fatal("expected metric key 'salary.min'")
	}
	if val.(float64) != 75000.0 {
		t.Errorf("got min %v, want 75000", val)
	}
}

func TestAggregate_Max(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "max", Field: "salary"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "salary.max")
	if !ok {
		t.Fatal("expected metric key 'salary.max'")
	}
	if val.(float64) != 120000.0 {
		t.Errorf("got max %v, want 120000", val)
	}
}

func TestAggregate_Sum(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "sum", Field: "salary"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "salary.sum")
	if !ok {
		t.Fatal("expected metric key 'salary.sum'")
	}
	if val.(float64) != 465000.0 {
		t.Errorf("got sum %v, want 465000", val)
	}
}

func TestAggregate_Avg(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "avg", Field: "salary"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "salary.avg")
	if !ok {
		t.Fatal("expected metric key 'salary.avg'")
	}
	if val.(float64) != 93000.0 {
		t.Errorf("got avg %v, want 93000", val)
	}
}

func TestAggregate_MultipleAggs(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "min", Field: "salary"},
			{Type: "max", Field: "salary"},
			{Type: "avg", Field: "salary"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	metrics := resp.Data[0].Metrics
	minVal, ok := findMetric(metrics, "salary.min")
	if !ok || minVal.(float64) != 75000.0 {
		t.Errorf("got min %v, want 75000", minVal)
	}
	maxVal, ok := findMetric(metrics, "salary.max")
	if !ok || maxVal.(float64) != 120000.0 {
		t.Errorf("got max %v, want 120000", maxVal)
	}
	avgVal, ok := findMetric(metrics, "salary.avg")
	if !ok || avgVal.(float64) != 93000.0 {
		t.Errorf("got avg %v, want 93000", avgVal)
	}
}

// --- GroupBy exact (11-14) ---

func TestAggregate_GroupByExact_Count(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	// We should have 3 groups: engineering, sales, hr.
	if len(resp.Data) != 3 {
		t.Fatalf("got %d groups, want 3", len(resp.Data))
	}

	// Build a map of department -> count for easier testing.
	groupCounts := make(map[string]uint64)
	for _, row := range resp.Data {
		dept := row.Group["department"].(string)
		count, ok := findMetric(row.Metrics, "count")
		if !ok {
			t.Fatalf("missing count metric for group %v", row.Group)
		}
		groupCounts[dept] = count.(uint64)
	}

	if groupCounts["engineering"] != 2 {
		t.Errorf("engineering count = %d, want 2", groupCounts["engineering"])
	}
	if groupCounts["sales"] != 2 {
		t.Errorf("sales count = %d, want 2", groupCounts["sales"])
	}
	if groupCounts["hr"] != 1 {
		t.Errorf("hr count = %d, want 1", groupCounts["hr"])
	}
}

func TestAggregate_GroupByExact_Avg(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "avg", Field: "salary"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	groupAvg := make(map[string]float64)
	for _, row := range resp.Data {
		dept := row.Group["department"].(string)
		val, ok := findMetric(row.Metrics, "salary.avg")
		if !ok {
			t.Fatalf("missing salary.avg metric for group %v", row.Group)
		}
		groupAvg[dept] = val.(float64)
	}

	// engineering: (100000+120000)/2 = 110000
	if groupAvg["engineering"] != 110000.0 {
		t.Errorf("engineering avg = %v, want 110000", groupAvg["engineering"])
	}
	// sales: (80000+90000)/2 = 85000
	if groupAvg["sales"] != 85000.0 {
		t.Errorf("sales avg = %v, want 85000", groupAvg["sales"])
	}
	// hr: 75000
	if groupAvg["hr"] != 75000.0 {
		t.Errorf("hr avg = %v, want 75000", groupAvg["hr"])
	}
}

func TestAggregate_GroupByExact_MultipleMetrics(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
			{Type: "avg", Field: "salary"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	for _, row := range resp.Data {
		if _, ok := findMetric(row.Metrics, "count"); !ok {
			t.Errorf("group %v missing 'count' metric", row.Group)
		}
		if _, ok := findMetric(row.Metrics, "salary.avg"); !ok {
			t.Errorf("group %v missing 'salary.avg' metric", row.Group)
		}
	}
}

func TestAggregate_GroupByExact_NoResults(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "nonexistent_field"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	if len(resp.Data) != 0 {
		t.Errorf("got %d groups, want 0 for non-existent field", len(resp.Data))
	}
}

// --- GroupBy ranges (15-17) ---

func TestAggregate_GroupByRanges(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	start80k := 80000.0
	end80k := 80000.0
	end100k1 := 100000.0 + 1 // exclusive upper, so use 100001 to include 100000
	start100k1 := 100000.0 + 1

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{
				Type:  "ranges",
				Field: "salary",
				Ranges: []Range{
					{Name: "low", EndValue: &end80k},                        // < 80000
					{Name: "mid", StartValue: &start80k, EndValue: &end100k1}, // [80000, 100001) i.e. 80000-100000
					{Name: "high", StartValue: &start100k1},                  // >= 100001
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	rangeCounts := make(map[string]uint64)
	for _, row := range resp.Data {
		name := row.Group["salary"].(string)
		count, ok := findMetric(row.Metrics, "count")
		if !ok {
			t.Fatalf("missing count metric for range %s", name)
		}
		rangeCounts[name] = count.(uint64)
	}

	// low (<80k): salary 75000 => 1
	if rangeCounts["low"] != 1 {
		t.Errorf("low count = %d, want 1", rangeCounts["low"])
	}
	// mid (80k-100k inclusive): salary 80000, 90000, 100000 => 3
	if rangeCounts["mid"] != 3 {
		t.Errorf("mid count = %d, want 3", rangeCounts["mid"])
	}
	// high (>100k): salary 120000 => 1
	if rangeCounts["high"] != 1 {
		t.Errorf("high count = %d, want 1", rangeCounts["high"])
	}
}

func TestAggregate_GroupByFixedWidth(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	width := 10.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "fixedWidth", Field: "age", Width: &width},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	// Ages: 28, 30, 32, 35, 40
	// With width 10: [20,30) has 28, [30,40) has 30,32,35, [40,50) has 40
	if len(resp.Data) < 2 {
		t.Fatalf("got %d groups, want at least 2", len(resp.Data))
	}

	// Verify total count across all groups sums to 5.
	total := uint64(0)
	for _, row := range resp.Data {
		count, ok := findMetric(row.Metrics, "count")
		if !ok {
			t.Fatalf("missing count metric for group %v", row.Group)
		}
		total += count.(uint64)
	}
	if total != 5 {
		t.Errorf("total count across groups = %d, want 5", total)
	}
}

func TestAggregate_GroupByRanges_Empty(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	lt10 := 10.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{
				Type:  "ranges",
				Field: "salary",
				Ranges: []Range{
					{Name: "none", EndValue: &lt10},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	// No documents have salary < 10, so no groups should be returned.
	if len(resp.Data) != 0 {
		t.Errorf("got %d groups, want 0 for empty range", len(resp.Data))
	}
}

// --- ApproximateDistinct (18-19) ---

func TestAggregate_ApproximateDistinct(t *testing.T) {
	idx := setupAggIndex(t)
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
		t.Fatal("expected metric key 'department.approximateDistinct'")
	}
	if val.(int) != 3 {
		t.Errorf("got distinct %v, want 3", val)
	}
}

func TestAggregate_ApproximateDistinct_SingleValue(t *testing.T) {
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("status", mapping.NewTextFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "single"), indexMapping)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	for i := 0; i < 5; i++ {
		idx.Index(string(rune('a'+i)), map[string]interface{}{"status": "active"})
	}

	eng := NewEngine()
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "status"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "status.approximateDistinct")
	if !ok {
		t.Fatal("expected metric key 'status.approximateDistinct'")
	}
	if val.(int) != 1 {
		t.Errorf("got distinct %v, want 1", val)
	}
}

// --- Response format (20-22) ---

func TestAggregationResponse_JSON(t *testing.T) {
	resp := AggregationResponse{
		ExcludedItems: 0,
		Data: []AggregationRow{
			{
				Metrics: []MetricValue{
					{Name: "count", Value: uint64(5)},
				},
			},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	if _, ok := decoded["excludedItems"]; !ok {
		t.Error("missing excludedItems in JSON")
	}
	if _, ok := decoded["data"]; !ok {
		t.Error("missing data in JSON")
	}
}

func TestAggregationRow_WithGroup(t *testing.T) {
	row := AggregationRow{
		Group: map[string]interface{}{
			"department": "engineering",
		},
		Metrics: []MetricValue{
			{Name: "count", Value: uint64(2)},
		},
	}

	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	group, ok := decoded["group"].(map[string]interface{})
	if !ok {
		t.Fatal("missing or invalid group in JSON")
	}
	if group["department"] != "engineering" {
		t.Errorf("got department %v, want engineering", group["department"])
	}
}

func TestAggregationRow_MetricsOnly(t *testing.T) {
	row := AggregationRow{
		Metrics: []MetricValue{
			{Name: "count", Value: uint64(5)},
		},
	}

	data, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}

	// Group should be omitted when nil.
	if _, ok := decoded["group"]; ok {
		t.Error("expected group to be omitted in JSON when nil")
	}
}

// --- Edge cases (23-25) ---

func TestAggregate_NilMetrics(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "min", Field: "nonexistent_field"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "nonexistent_field.min")
	if !ok {
		t.Fatal("expected metric key 'nonexistent_field.min'")
	}
	if val != nil {
		t.Errorf("expected nil for non-existent field, got %v", val)
	}
}

func TestAggregate_EmptyIndex(t *testing.T) {
	idx := setupEmptyIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
			{Type: "min", Field: "salary"},
			{Type: "max", Field: "salary"},
			{Type: "sum", Field: "salary"},
			{Type: "avg", Field: "salary"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	metrics := resp.Data[0].Metrics

	countVal, ok := findMetric(metrics, "count")
	if !ok {
		t.Fatal("expected metric key 'count'")
	}
	if countVal.(uint64) != 0 {
		t.Errorf("got count %v, want 0", countVal)
	}
	// Numeric aggregations should return nil for empty index.
	for _, key := range []string{"salary.min", "salary.max", "salary.sum", "salary.avg"} {
		val, ok := findMetric(metrics, key)
		if !ok {
			t.Errorf("expected metric key %s", key)
			continue
		}
		if val != nil {
			t.Errorf("expected nil for %s on empty index, got %v", key, val)
		}
	}
}

func TestAggregate_NoAggregations(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	if len(resp.Data) != 1 {
		t.Fatalf("got %d rows, want 1", len(resp.Data))
	}
	if len(resp.Data[0].Metrics) != 0 {
		t.Errorf("got %d metrics, want 0", len(resp.Data[0].Metrics))
	}
}

// --- Accuracy and ComputeUsage (26 + US-382) ---

func TestAggregate_AccuracyAndComputeUsage(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	if resp.Accuracy != "ACCURATE" {
		t.Errorf("expected accuracy 'ACCURATE', got %q", resp.Accuracy)
	}
	if resp.ComputeUsage == nil {
		t.Fatalf("expected computeUsage to be populated, got nil")
	}
	// scannedRows must equal the index doc count visible to the base query.
	docCount, _ := idx.DocCount()
	if resp.ComputeUsage.ScannedRows != int64(docCount) {
		t.Errorf("expected computeUsage.scannedRows %d, got %d", docCount, resp.ComputeUsage.ScannedRows)
	}
	if resp.ComputeUsage.DurationMs < 0 {
		t.Errorf("expected non-negative durationMs, got %d", resp.ComputeUsage.DurationMs)
	}
	if resp.ComputeUsage.Accuracy != resp.Accuracy {
		t.Errorf("expected computeUsage.accuracy to mirror top-level accuracy %q, got %q", resp.Accuracy, resp.ComputeUsage.Accuracy)
	}

	// Verify it appears in JSON output
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if decoded["accuracy"] != "ACCURATE" {
		t.Errorf("expected accuracy 'ACCURATE' in JSON, got %v", decoded["accuracy"])
	}
	cu, ok := decoded["computeUsage"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected computeUsage to be a JSON object, got %T (%v)", decoded["computeUsage"], decoded["computeUsage"])
	}
	if cu["scannedRows"].(float64) != float64(docCount) {
		t.Errorf("expected computeUsage.scannedRows %d in JSON, got %v", docCount, cu["scannedRows"])
	}
	if _, ok := cu["durationMs"]; !ok {
		t.Errorf("missing computeUsage.durationMs in JSON")
	}
	if cu["accuracy"] != "ACCURATE" {
		t.Errorf("expected computeUsage.accuracy 'ACCURATE' in JSON, got %v", cu["accuracy"])
	}
}

// floatApproxEqual checks if two floats are approximately equal.
func floatApproxEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

// --- GroupBy range alias (27) ---

func TestAggregate_GroupByRange_Alias(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	low := 0.0
	mid := 100000.0
	high := 200000.0

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{
				Type:  "range", // singular alias
				Field: "salary",
				Ranges: []Range{
					{Name: "low", StartValue: &low, EndValue: &mid},  // [0, 100000)
					{Name: "high", StartValue: &mid, EndValue: &high}, // [100000, 200000)
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	rangeCounts := make(map[string]uint64)
	for _, row := range resp.Data {
		name := row.Group["salary"].(string)
		count, ok := findMetric(row.Metrics, "count")
		if !ok {
			t.Fatalf("missing count metric for range %s", name)
		}
		rangeCounts[name] = count.(uint64)
	}

	// low [0, 100000): salary 75000, 80000, 90000 => 3
	if rangeCounts["low"] != 3 {
		t.Errorf("low count = %d, want 3", rangeCounts["low"])
	}
	// high [100000, 200000): salary 100000, 120000 => 2
	if rangeCounts["high"] != 2 {
		t.Errorf("high count = %d, want 2", rangeCounts["high"])
	}
}

// --- GroupBy topValues (28-30) ---

func TestAggregate_GroupByTopValues_Basic(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "topValues", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	// 3 distinct departments: engineering(2), sales(2), hr(1)
	if len(resp.Data) != 3 {
		t.Fatalf("got %d groups, want 3", len(resp.Data))
	}

	groupCounts := make(map[string]uint64)
	for _, row := range resp.Data {
		dept := row.Group["department"].(string)
		count, ok := findMetric(row.Metrics, "count")
		if !ok {
			t.Fatalf("missing count metric for group %v", row.Group)
		}
		groupCounts[dept] = count.(uint64)
	}

	if groupCounts["engineering"] != 2 {
		t.Errorf("engineering count = %d, want 2", groupCounts["engineering"])
	}
	if groupCounts["sales"] != 2 {
		t.Errorf("sales count = %d, want 2", groupCounts["sales"])
	}
	if groupCounts["hr"] != 1 {
		t.Errorf("hr count = %d, want 1", groupCounts["hr"])
	}
}

func TestAggregate_GroupByTopValues_MaxGroupCount(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	maxGroups := 2
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "topValues", Field: "department", MaxGroups: &maxGroups},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	// maxGroupCount=2 means only top 2 by frequency are returned
	if len(resp.Data) != 2 {
		t.Fatalf("got %d groups, want 2 (maxGroupCount=2)", len(resp.Data))
	}

	// The top 2 by frequency should be engineering(2) and sales(2); hr(1) is excluded.
	for _, row := range resp.Data {
		dept := row.Group["department"].(string)
		if dept == "hr" {
			t.Errorf("expected hr to be excluded from top 2, but it was included")
		}
	}
}

func TestAggregate_GroupByTopValues_Empty(t *testing.T) {
	idx := setupEmptyIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "topValues", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	if len(resp.Data) != 0 {
		t.Errorf("got %d groups, want 0 for empty index", len(resp.Data))
	}
}

// --- GroupBy duration with DurationValue format (31-32) ---

func setupTimestampIndex(t *testing.T) bleve.Index {
	t.Helper()
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("name", mapping.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("value", mapping.NewNumericFieldMapping())
	docMapping.AddFieldMappingsAt("startDate", mapping.NewNumericFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "ts"), indexMapping)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	// Create docs with startDate as epoch seconds in two distinct 30-day buckets.
	// Day 0: epoch 0 (1970-01-01)
	// Day 31: epoch 31*86400 (1970-02-01) -> different 30-day bucket
	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"1", map[string]interface{}{"name": "a", "value": 1.0, "startDate": 0.0}},
		{"2", map[string]interface{}{"name": "b", "value": 2.0, "startDate": float64(10 * 86400)}},   // day 10, same bucket as doc1
		{"3", map[string]interface{}{"name": "c", "value": 3.0, "startDate": float64(31 * 86400)}},  // day 31, new bucket
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("failed to index doc %s: %v", d.id, err)
		}
	}
	return idx
}

func TestAggregate_GroupByDuration_DurationValueFormat(t *testing.T) {
	idx := setupTimestampIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{
				Type:  "duration",
				Field: "startDate",
				DurationValue: &DurationValue{Unit: "DAYS", Value: 30},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	// docs 1,2 fall in the [0, 30d) bucket; doc 3 falls in the [31d, 61d) bucket.
	if len(resp.Data) != 2 {
		t.Fatalf("got %d buckets, want 2", len(resp.Data))
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
		t.Errorf("total count across duration buckets = %d, want 3", total)
	}
}

func TestAggregate_GroupByDuration_MissingDuration(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	// Neither Duration nor DurationValue set — should return error.
	_, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count"}},
		GroupBy: []GroupBySpec{
			{Type: "duration", Field: "salary"},
		},
	})
	if err == nil {
		t.Fatal("expected error for duration groupBy with no duration spec, got nil")
	}
}

// --- DurationValue unit conversion (33) ---

func TestDurationValueToSeconds(t *testing.T) {
	cases := []struct {
		dv      DurationValue
		wantSec float64
	}{
		{DurationValue{Unit: "SECONDS", Value: 60}, 60},
		{DurationValue{Unit: "MINUTES", Value: 2}, 120},
		{DurationValue{Unit: "HOURS", Value: 1}, 3600},
		{DurationValue{Unit: "DAYS", Value: 1}, 86400},
		{DurationValue{Unit: "WEEKS", Value: 1}, 7 * 86400},
		{DurationValue{Unit: "MONTHS", Value: 1}, 30 * 86400},
		{DurationValue{Unit: "YEARS", Value: 1}, 365 * 86400},
	}
	for _, tc := range cases {
		got, err := durationValueToSeconds(&tc.dv)
		if err != nil {
			t.Errorf("durationValueToSeconds(%v) returned error: %v", tc.dv, err)
			continue
		}
		if got != tc.wantSec {
			t.Errorf("durationValueToSeconds(%v) = %v, want %v", tc.dv, got, tc.wantSec)
		}
	}

	// Unknown unit
	_, err := durationValueToSeconds(&DurationValue{Unit: "FORTNIGHTS", Value: 1})
	if err == nil {
		t.Error("expected error for unknown unit, got nil")
	}
}

// --- Accuracy APPROXIMATE when truncated ---

func TestAggregate_Duration_Accuracy_Approximate_WhenTruncated(t *testing.T) {
	idx := setupAggIndex(t) // 5 docs
	eng := NewEngine()
	eng.MaxDocScanSize = 3 // only scan 3 of 5

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "duration", Field: "age", Duration: "P1Y"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	if resp.Accuracy != "APPROXIMATE" {
		t.Errorf("got accuracy %q, want APPROXIMATE", resp.Accuracy)
	}
}

func TestAggregate_Duration_Accuracy_Accurate_WhenNotTruncated(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine() // default MaxDocScanSize=10000, 5 docs won't truncate

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "duration", Field: "age", Duration: "P1Y"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	if resp.Accuracy != "ACCURATE" {
		t.Errorf("got accuracy %q, want ACCURATE", resp.Accuracy)
	}
}

func TestAggregate_FixedWidth_Accuracy_Approximate_WhenTruncated(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()
	eng.MaxDocScanSize = 3

	width := 10.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "fixedWidth", Field: "age", Width: &width},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}
	if resp.Accuracy != "APPROXIMATE" {
		t.Errorf("got accuracy %q, want APPROXIMATE", resp.Accuracy)
	}
}

// --- Multi-field groupBy ---

func setupMultiGroupByIndex(t *testing.T) bleve.Index {
	t.Helper()
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("department", mapping.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("level", mapping.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("salary", mapping.NewNumericFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "multigb"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	docs := []struct {
		id  string
		doc map[string]interface{}
	}{
		{"1", map[string]interface{}{"department": "eng", "level": "senior", "salary": 120000.0}},
		{"2", map[string]interface{}{"department": "eng", "level": "junior", "salary": 80000.0}},
		{"3", map[string]interface{}{"department": "eng", "level": "junior", "salary": 85000.0}},
		{"4", map[string]interface{}{"department": "sales", "level": "senior", "salary": 100000.0}},
		{"5", map[string]interface{}{"department": "sales", "level": "junior", "salary": 70000.0}},
	}
	for _, d := range docs {
		if err := idx.Index(d.id, d.doc); err != nil {
			t.Fatalf("index doc %s: %v", d.id, err)
		}
	}
	return idx
}

func TestAggregate_MultipleGroupBy_ExactExact(t *testing.T) {
	idx := setupMultiGroupByIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
			{Type: "exact", Field: "level"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	// Expected groups: eng/senior(1), eng/junior(2), sales/senior(1), sales/junior(1)
	if len(resp.Data) != 4 {
		t.Fatalf("got %d groups, want 4; data=%+v", len(resp.Data), resp.Data)
	}

	// Build map of "dept|level" -> count
	groupCounts := make(map[string]uint64)
	for _, row := range resp.Data {
		dept := row.Group["department"].(string)
		level := row.Group["level"].(string)
		count, ok := findMetric(row.Metrics, "count")
		if !ok {
			t.Fatalf("missing count for group %v", row.Group)
		}
		groupCounts[dept+"|"+level] = count.(uint64)
	}

	if groupCounts["eng|senior"] != 1 {
		t.Errorf("eng|senior count = %d, want 1", groupCounts["eng|senior"])
	}
	if groupCounts["eng|junior"] != 2 {
		t.Errorf("eng|junior count = %d, want 2", groupCounts["eng|junior"])
	}
	if groupCounts["sales|senior"] != 1 {
		t.Errorf("sales|senior count = %d, want 1", groupCounts["sales|senior"])
	}
	if groupCounts["sales|junior"] != 1 {
		t.Errorf("sales|junior count = %d, want 1", groupCounts["sales|junior"])
	}
}

func TestAggregate_MultipleGroupBy_Metrics(t *testing.T) {
	idx := setupMultiGroupByIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "avg", Field: "salary"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
			{Type: "exact", Field: "level"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	// Build map of "dept|level" -> avg salary
	groupAvg := make(map[string]float64)
	for _, row := range resp.Data {
		dept := row.Group["department"].(string)
		level := row.Group["level"].(string)
		val, ok := findMetric(row.Metrics, "salary.avg")
		if !ok {
			t.Fatalf("missing salary.avg for group %v", row.Group)
		}
		groupAvg[dept+"|"+level] = val.(float64)
	}

	// eng/junior: (80000+85000)/2 = 82500
	if groupAvg["eng|junior"] != 82500.0 {
		t.Errorf("eng|junior avg = %v, want 82500", groupAvg["eng|junior"])
	}
	// eng/senior: 120000
	if groupAvg["eng|senior"] != 120000.0 {
		t.Errorf("eng|senior avg = %v, want 120000", groupAvg["eng|senior"])
	}
}

// --- ExactDistinct metric ---

func TestAggregate_ExactDistinct_FiveUniqueValues(t *testing.T) {
	// Create index with documents having 5 unique city values.
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("city", mapping.NewTextFieldMapping())
	docMapping.AddFieldMappingsAt("score", mapping.NewNumericFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "exact_distinct"), indexMapping)
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	cities := []string{"newyork", "london", "tokyo", "paris", "berlin"}
	for i, city := range cities {
		if err := idx.Index(fmt.Sprintf("doc%d", i), map[string]interface{}{
			"city":  city,
			"score": float64(i * 10),
		}); err != nil {
			t.Fatalf("failed to index doc%d: %v", i, err)
		}
	}

	eng := NewEngine()
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "exactDistinct", Field: "city"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "city.exactDistinct")
	if !ok {
		t.Fatal("expected metric key 'city.exactDistinct'")
	}
	if val.(int) != 5 {
		t.Errorf("got exactDistinct %v, want 5", val)
	}
}

// --- collectList metric ---

func TestAggregate_CollectList_GroupByCategory(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "collectList", Field: "salary"},
		},
		GroupBy: []GroupBySpec{
			{Type: "exact", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	// 3 departments: engineering(2 salaries), sales(2 salaries), hr(1 salary)
	if len(resp.Data) != 3 {
		t.Fatalf("got %d groups, want 3", len(resp.Data))
	}

	groupLists := make(map[string][]interface{})
	for _, row := range resp.Data {
		dept := row.Group["department"].(string)
		val, ok := findMetric(row.Metrics, "salary.collectList")
		if !ok {
			t.Fatalf("missing salary.collectList metric for group %v", row.Group)
		}
		list, ok := val.([]interface{})
		if !ok {
			t.Fatalf("expected []interface{} for collectList, got %T", val)
		}
		groupLists[dept] = list
	}

	// engineering: salaries 100000, 120000
	if len(groupLists["engineering"]) != 2 {
		t.Errorf("engineering collectList length = %d, want 2", len(groupLists["engineering"]))
	}
	// sales: salaries 80000, 90000
	if len(groupLists["sales"]) != 2 {
		t.Errorf("sales collectList length = %d, want 2", len(groupLists["sales"]))
	}
	// hr: salary 75000
	if len(groupLists["hr"]) != 1 {
		t.Errorf("hr collectList length = %d, want 1", len(groupLists["hr"]))
	}
}

func TestAggregate_CollectList_MaxItems(t *testing.T) {
	idx := setupAggIndex(t) // 5 docs total
	eng := NewEngine()

	maxItems := 3
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "collectList", Field: "salary", MaxItems: &maxItems},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "salary.collectList")
	if !ok {
		t.Fatal("expected metric key 'salary.collectList'")
	}
	list, ok := val.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{} for collectList, got %T", val)
	}
	if len(list) != 3 {
		t.Errorf("got collectList length %d, want 3 (maxItems=3)", len(list))
	}
}

func TestAggregate_CollectList_DefaultMaxItems(t *testing.T) {
	idx := setupAggIndex(t) // 5 docs
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "collectList", Field: "salary"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "salary.collectList")
	if !ok {
		t.Fatal("expected metric key 'salary.collectList'")
	}
	list, ok := val.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{} for collectList, got %T", val)
	}
	// Default maxItems=100, only 5 docs, so all 5 should be collected
	if len(list) != 5 {
		t.Errorf("got collectList length %d, want 5", len(list))
	}
}

func TestAggregate_CollectList_StringField(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "collectList", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	val, ok := findMetric(resp.Data[0].Metrics, "department.collectList")
	if !ok {
		t.Fatal("expected metric key 'department.collectList'")
	}
	list, ok := val.([]interface{})
	if !ok {
		t.Fatalf("expected []interface{} for collectList, got %T", val)
	}
	// 5 docs, all have department field
	if len(list) != 5 {
		t.Errorf("got collectList length %d, want 5", len(list))
	}
}

func TestAggregate_ExactDistinct_VsApproximateDistinct(t *testing.T) {
	idx := setupAggIndex(t) // 3 unique departments
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "exactDistinct", Field: "department"},
			{Type: "approximateDistinct", Field: "department"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate returned error: %v", err)
	}

	exactVal, ok := findMetric(resp.Data[0].Metrics, "department.exactDistinct")
	if !ok {
		t.Fatal("expected metric key 'department.exactDistinct'")
	}
	approxVal, ok := findMetric(resp.Data[0].Metrics, "department.approximateDistinct")
	if !ok {
		t.Fatal("expected metric key 'department.approximateDistinct'")
	}

	exactCount := exactVal.(int)
	approxCount := approxVal.(int)

	// Both should return 3 for this small dataset
	if exactCount != 3 {
		t.Errorf("exactDistinct = %d, want 3", exactCount)
	}
	// Exact is always precise; approximate may under/over-count on large datasets
	// but for small datasets they should agree
	if exactCount != approxCount {
		t.Errorf("exactDistinct (%d) != approximateDistinct (%d) — for small datasets they should agree", exactCount, approxCount)
	}
}
