package aggregation

import (
	"encoding/json"
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

// --- Accuracy and ComputeUsage (26) ---

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
	if resp.ComputeUsage != 4.0 {
		t.Errorf("expected computeUsage 4.0, got %v", resp.ComputeUsage)
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
	if decoded["computeUsage"].(float64) != 4.0 {
		t.Errorf("expected computeUsage 4.0 in JSON, got %v", decoded["computeUsage"])
	}
}

// floatApproxEqual checks if two floats are approximately equal.
func floatApproxEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}
