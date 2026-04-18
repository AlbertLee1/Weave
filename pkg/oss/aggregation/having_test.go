package aggregation

import (
	"testing"
)

func TestHaving_ValidationRejectsMissingMetric(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	_, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
		Having:       []HavingClause{{Op: "gt", Value: 0}},
	})
	if err == nil {
		t.Fatalf("expected validation error for missing metric name")
	}
}

func TestHaving_ValidationRejectsUnknownOp(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	_, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
		Having:       []HavingClause{{Metric: "n", Op: "like", Value: 0}},
	})
	if err == nil {
		t.Fatalf("expected validation error for unsupported op")
	}
}

// TestHaving_FiltersGroupByRows drops buckets whose per-group count fails
// the Having threshold.
func TestHaving_FiltersGroupByRows(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
		GroupBy:      []GroupBySpec{{Type: "exact", Field: "department"}},
		Having:       []HavingClause{{Metric: "n", Op: "gte", Value: 2}},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	// Fixture: engineering=2, sales=2, hr=1 — hr row should be filtered out.
	if len(resp.Data) != 2 {
		t.Fatalf("expected 2 rows after having, got %d (%+v)", len(resp.Data), resp.Data)
	}
	for _, row := range resp.Data {
		n, _ := findMetric(row.Metrics, "n")
		if n.(uint64) < 2 {
			t.Errorf("row %v slipped through: n=%v", row.Group, n)
		}
		if dep, _ := row.Group["department"].(string); dep == "hr" {
			t.Errorf("hr row should have been filtered out: %+v", row)
		}
	}
}

// TestHaving_OnSimpleAggregation filters the single simple-aggregation row.
// A clause the sole row fails removes every row from the response.
func TestHaving_OnSimpleAggregation(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
		Having:       []HavingClause{{Metric: "n", Op: "gt", Value: 100}},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("expected empty data (count=5 fails >100), got %+v", resp.Data)
	}

	// Inverse: a passing clause keeps the row.
	resp, err = eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
		Having:       []HavingClause{{Metric: "n", Op: "gte", Value: 1}},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 row, got %+v", resp.Data)
	}
}

// TestHaving_MultipleClausesAnd requires every clause to match.
func TestHaving_MultipleClausesAnd(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count", Name: "n"},
			{Type: "avg", Field: "salary", Name: "avgSalary"},
		},
		GroupBy: []GroupBySpec{{Type: "exact", Field: "department"}},
		Having: []HavingClause{
			{Metric: "n", Op: "gte", Value: 2},
			{Metric: "avgSalary", Op: "gt", Value: 100000},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}

	// Fixture averages: engineering=110k, sales=85k, hr=75k. Only engineering
	// has n>=2 AND avg>100k.
	if len(resp.Data) != 1 {
		t.Fatalf("expected 1 row, got %d (%+v)", len(resp.Data), resp.Data)
	}
	dep, _ := resp.Data[0].Group["department"].(string)
	if dep != "engineering" {
		t.Fatalf("expected engineering, got %q", dep)
	}
}

// TestHaving_OnSubAggregation filters leaf rows of a sub-aggregation
// independently of the outer response.
func TestHaving_OnSubAggregation(t *testing.T) {
	idx := setupSubAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "all"}},
		SubAggregations: []SubAggregationSpec{
			{
				Name:         "byCountry",
				Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
				GroupBy:      []GroupBySpec{{Type: "exact", Field: "country"}},
				Having:       []HavingClause{{Metric: "n", Op: "gt", Value: 3}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	sub := resp.SubAggregations["byCountry"]
	if sub == nil {
		t.Fatalf("missing byCountry sub-agg")
	}

	// Both country buckets have count=3 → having n>3 drops everything.
	if len(sub.Data) != 0 {
		t.Fatalf("expected 0 buckets after having, got %+v", sub.Data)
	}
}

// TestHaving_MissingMetricFailsClause: if the named metric does not exist on
// the row, the row is dropped (safe default — prevents accidental match).
func TestHaving_MissingMetricFailsClause(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{{Type: "count", Name: "n"}},
		GroupBy:      []GroupBySpec{{Type: "exact", Field: "department"}},
		Having:       []HavingClause{{Metric: "doesNotExist", Op: "gte", Value: 0}},
	})
	if err != nil {
		t.Fatalf("Aggregate error: %v", err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("expected empty data when metric missing, got %+v", resp.Data)
	}
}

// TestHaving_OpsCoverage exercises each supported comparison operator.
func TestHaving_OpsCoverage(t *testing.T) {
	cases := []struct {
		op    string
		value float64
		want  bool // whether a row with metric=5 should pass
	}{
		{"eq", 5, true},
		{"eq", 4, false},
		{"ne", 5, false},
		{"ne", 4, true},
		{"gt", 4, true},
		{"gt", 5, false},
		{"gte", 5, true},
		{"gte", 6, false},
		{"lt", 6, true},
		{"lt", 5, false},
		{"lte", 5, true},
		{"lte", 4, false},
	}
	row := AggregationRow{Metrics: []MetricValue{{Name: "m", Value: uint64(5)}}}
	for _, tc := range cases {
		got := rowMatchesHaving(row, []HavingClause{{Metric: "m", Op: tc.op, Value: tc.value}})
		if got != tc.want {
			t.Errorf("op=%s value=%g: want %v, got %v", tc.op, tc.value, tc.want, got)
		}
	}
}
