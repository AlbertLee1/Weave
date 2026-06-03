package aggregation

import (
	"testing"
)

// metricVals pulls the named metric value from each row in order, coerced to
// float64.
func metricVals(t *testing.T, resp *AggregationResponse, name string) []float64 {
	t.Helper()
	out := make([]float64, 0, len(resp.Data))
	for _, row := range resp.Data {
		v, ok := findMetric(row.Metrics, name)
		if !ok {
			t.Fatalf("row %v missing metric %q", row.Group, name)
		}
		f, ok := toFloat(v)
		if !ok {
			t.Fatalf("metric %q value %v (%T) not numeric", name, v, v)
		}
		out = append(out, f)
	}
	return out
}

func toFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

// TestAggregate_MetricDirection_OrdersGroupRows is the Palantir "按聚合值排序"
// contract (syntax ref L463 / L623): an aggregation metric carrying a
// direction orders the groupBy result rows by that metric's value.
//
// Fixture sums (setupAggIndex): engineering=220000, sales=170000, hr=75000.
func TestAggregate_MetricDirection_OrdersGroupRows(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	run := func(dir string) []float64 {
		resp, err := eng.Aggregate(idx, &AggregationRequest{
			Aggregations: []AggregationSpec{
				{Type: "sum", Field: "salary", Name: "total", Direction: dir},
			},
			GroupBy: []GroupBySpec{{Type: "exact", Field: "department"}},
		})
		if err != nil {
			t.Fatalf("Aggregate(dir=%q) error: %v", dir, err)
		}
		return metricVals(t, resp, "total")
	}

	asc := run("ASC")
	wantAsc := []float64{75000, 170000, 220000}
	for i := range wantAsc {
		if i >= len(asc) || asc[i] != wantAsc[i] {
			t.Fatalf("ASC order = %v, want %v", asc, wantAsc)
		}
	}

	desc := run("DESC")
	wantDesc := []float64{220000, 170000, 75000}
	for i := range wantDesc {
		if i >= len(desc) || desc[i] != wantDesc[i] {
			t.Fatalf("DESC order = %v, want %v", desc, wantDesc)
		}
	}
}

// TestAggregate_MetricDirection_Invalid rejects a bogus direction so callers
// get a clean error instead of silent no-op ordering.
func TestAggregate_MetricDirection_Invalid(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	_, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "sum", Field: "salary", Name: "total", Direction: "SIDEWAYS"},
		},
		GroupBy: []GroupBySpec{{Type: "exact", Field: "department"}},
	})
	if err == nil {
		t.Fatal("expected error for invalid direction, got nil")
	}
}
