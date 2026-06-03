package aggregation

import (
	"testing"
)

func sumAllMetric(t *testing.T, resp *AggregationResponse, name string) float64 {
	t.Helper()
	total := 0.0
	for _, v := range metricVals(t, resp, name) {
		total += v
	}
	return total
}

// TestAggregate_MetricPropertyIdentifier_ResolvesToField is the syntax-ref
// L461 contract: a metric may name its target via propertyIdentifier instead
// of a bare field, and must produce the identical result.
//
// Fixture salaries (setupAggIndex) sum to 465000 across all departments.
func TestAggregate_MetricPropertyIdentifier_ResolvesToField(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	pi := &PropertyIdentifier{}
	pi.Property.APIName = "salary"

	viaPI, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "sum", Name: "total", PropertyIdentifier: pi},
		},
		GroupBy: []GroupBySpec{{Type: "exact", Field: "department"}},
	})
	if err != nil {
		t.Fatalf("Aggregate(propertyIdentifier) error: %v", err)
	}
	if got := sumAllMetric(t, viaPI, "total"); got != 465000 {
		t.Fatalf("sum via propertyIdentifier = %v, want 465000 (resolved to field salary)", got)
	}
}

// TestAggregate_MetricPropertyIdentifier_MutuallyExclusive rejects a metric
// that sets both field and a conflicting propertyIdentifier.
func TestAggregate_MetricPropertyIdentifier_MutuallyExclusive(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()

	pi := &PropertyIdentifier{}
	pi.Property.APIName = "age"

	_, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "sum", Field: "salary", PropertyIdentifier: pi, Name: "x"},
		},
	})
	if err == nil {
		t.Fatal("expected error when both field and propertyIdentifier are set, got nil")
	}
}
