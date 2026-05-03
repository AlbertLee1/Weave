package aggregation

// MetricValue represents a single named metric value in the Palantir V2 format.
type MetricValue struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

// ComputeUsage is the per-aggregation cost-and-quality envelope (US-382).
// Palantir V2 wire shape: {scannedRows, durationMs, accuracy}.
//   - ScannedRows is the post-exclusion total document count visible to the
//     aggregation engine (i.e. the input cardinality after excludedItems
//     pre-filter and ObjectSet base scoping). It is a lower bound on the
//     real I/O — per-bucket facet scans run on top of this set, but reporting
//     the input size is the contract callers actually use to reason about
//     query cost.
//   - DurationMs is wall-clock duration of AggregateWithQuery, measured from
//     entry to just before the ComputeUsage stamp.
//   - Accuracy mirrors the top-level AggregationResponse.Accuracy verdict
//     ("ACCURATE" or "APPROXIMATE"). Duplicating it here keeps the
//     compute-cost envelope self-describing for clients that index off
//     computeUsage alone.
type ComputeUsage struct {
	ScannedRows int64  `json:"scannedRows"`
	DurationMs  int64  `json:"durationMs"`
	Accuracy    string `json:"accuracy,omitempty"`
}

// AggregationResponse is the Palantir V2 aggregation response format.
type AggregationResponse struct {
	ExcludedItems   int                             `json:"excludedItems"`
	Accuracy        string                          `json:"accuracy,omitempty"`
	ComputeUsage    *ComputeUsage                   `json:"computeUsage,omitempty"`
	Data            []AggregationRow                `json:"data"`
	SubAggregations map[string]*AggregationResponse `json:"subAggregations,omitempty"`
}

// AggregationRow is a single row in the aggregation result.
type AggregationRow struct {
	Group           map[string]interface{}          `json:"group,omitempty"`
	Metrics         []MetricValue                   `json:"metrics"`
	SubAggregations map[string]*AggregationResponse `json:"subAggregations,omitempty"`
}
