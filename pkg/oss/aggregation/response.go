package aggregation

// MetricValue represents a single named metric value in the Palantir V2 format.
type MetricValue struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

// AggregationResponse is the Palantir V2 aggregation response format.
type AggregationResponse struct {
	ExcludedItems   int                             `json:"excludedItems"`
	Accuracy        string                          `json:"accuracy,omitempty"`
	ComputeUsage    float64                         `json:"computeUsage,omitempty"`
	Data            []AggregationRow                `json:"data"`
	SubAggregations map[string]*AggregationResponse `json:"subAggregations,omitempty"`
}

// AggregationRow is a single row in the aggregation result.
type AggregationRow struct {
	Group           map[string]interface{}          `json:"group,omitempty"`
	Metrics         []MetricValue                   `json:"metrics"`
	SubAggregations map[string]*AggregationResponse `json:"subAggregations,omitempty"`
}
