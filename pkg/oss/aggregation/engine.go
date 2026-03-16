package aggregation

import (
	"fmt"
	"math"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// AggregationRequest represents a Palantir V2 aggregation request.
type AggregationRequest struct {
	ObjectType   string               `json:"objectType"`
	Query        *bleve.SearchRequest `json:"-"` // pre-built search request (may be nil for all objects)
	Aggregations []AggregationSpec    `json:"aggregation"`
	GroupBy      []GroupBySpec        `json:"groupBy,omitempty"`
}

// AggregationSpec defines what to aggregate.
type AggregationSpec struct {
	Type       string   `json:"type"`                      // "count", "min", "max", "sum", "avg", "approximateDistinct", "standardDeviation", "variance", "approximatePercentile"
	Field      string   `json:"field,omitempty"`           // required for min/max/sum/avg
	Name       string   `json:"name,omitempty"`            // output name
	Percentile *float64 `json:"percentile,omitempty"`      // for approximatePercentile (0-100)
}

// GroupBySpec defines how to group results.
type GroupBySpec struct {
	Type      string   `json:"type"`                    // "exact", "fixedWidth", "ranges", "duration"
	Field     string   `json:"field"`
	MaxGroups *int     `json:"maxGroupCount,omitempty"`
	Width     *float64 `json:"fixedWidth,omitempty"`    // for fixedWidth
	Ranges    []Range  `json:"ranges,omitempty"`        // for ranges
	Duration  string   `json:"duration,omitempty"`      // ISO 8601: P1D, P1W, P1M, P1Y
}

// Range defines a range bucket using Palantir V2 startValue/endValue format.
type Range struct {
	Name       string   `json:"name,omitempty"`
	StartValue *float64 `json:"startValue,omitempty"` // inclusive
	EndValue   *float64 `json:"endValue,omitempty"`   // exclusive
}

// Engine computes aggregations.
type Engine struct{}

// NewEngine creates a new aggregation engine.
func NewEngine() *Engine {
	return &Engine{}
}

// Aggregate performs aggregation on the given index.
func (e *Engine) Aggregate(idx bleve.Index, req *AggregationRequest) (*AggregationResponse, error) {
	// Build base query (match all if no query).
	var baseQuery query.Query = bleve.NewMatchAllQuery()

	var resp *AggregationResponse
	var err error

	// If groupBy is specified, use Bleve facets.
	if len(req.GroupBy) > 0 {
		resp, err = e.aggregateWithGroupBy(idx, baseQuery, req)
	} else {
		// Simple aggregation without groupBy.
		resp, err = e.aggregateSimple(idx, baseQuery, req)
	}
	if err != nil {
		return nil, err
	}

	resp.Accuracy = "ACCURATE"
	resp.ComputeUsage = 4.0
	return resp, nil
}

// AggregateWithQuery performs aggregation on the given index with an explicit base query.
func (e *Engine) AggregateWithQuery(idx bleve.Index, baseQuery query.Query, req *AggregationRequest) (*AggregationResponse, error) {
	if baseQuery == nil {
		baseQuery = bleve.NewMatchAllQuery()
	}

	var resp *AggregationResponse
	var err error

	// If groupBy is specified, use Bleve facets.
	if len(req.GroupBy) > 0 {
		resp, err = e.aggregateWithGroupBy(idx, baseQuery, req)
	} else {
		// Simple aggregation without groupBy.
		resp, err = e.aggregateSimple(idx, baseQuery, req)
	}
	if err != nil {
		return nil, err
	}

	resp.Accuracy = "ACCURATE"
	resp.ComputeUsage = 4.0
	return resp, nil
}

// aggregateSimple performs aggregation without groupBy.
func (e *Engine) aggregateSimple(idx bleve.Index, baseQuery query.Query, req *AggregationRequest) (*AggregationResponse, error) {
	metrics, err := computeMetrics(idx, baseQuery, req.Aggregations)
	if err != nil {
		return nil, fmt.Errorf("compute metrics: %w", err)
	}

	return &AggregationResponse{
		Data: []AggregationRow{
			{Metrics: metrics},
		},
	}, nil
}

// aggregateWithGroupBy performs aggregation with groupBy using Bleve facets.
func (e *Engine) aggregateWithGroupBy(idx bleve.Index, baseQuery query.Query, req *AggregationRequest) (*AggregationResponse, error) {
	if len(req.GroupBy) == 0 {
		return nil, fmt.Errorf("groupBy is empty")
	}

	// We only support a single groupBy for now.
	gb := req.GroupBy[0]

	switch gb.Type {
	case "exact":
		return e.groupByExact(idx, baseQuery, gb, req.Aggregations)
	case "fixedWidth":
		return e.groupByFixedWidth(idx, baseQuery, gb, req.Aggregations)
	case "ranges":
		return e.groupByRanges(idx, baseQuery, gb, req.Aggregations)
	case "duration":
		return e.groupByDuration(idx, baseQuery, gb, req.Aggregations)
	default:
		return nil, fmt.Errorf("unsupported groupBy type: %q", gb.Type)
	}
}

// groupByExact uses Bleve TermsFacet to group by exact field values.
func (e *Engine) groupByExact(idx bleve.Index, baseQuery query.Query, gb GroupBySpec, specs []AggregationSpec) (*AggregationResponse, error) {
	maxGroups := 100
	if gb.MaxGroups != nil {
		maxGroups = *gb.MaxGroups
	}

	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = 0
	facet := bleve.NewFacetRequest(gb.Field, maxGroups)
	searchReq.AddFacet("groupby", facet)

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("facet search: %w", err)
	}

	facetResult, ok := result.Facets["groupby"]
	if !ok {
		return &AggregationResponse{Data: []AggregationRow{}}, nil
	}

	if facetResult.Terms == nil {
		return &AggregationResponse{Data: []AggregationRow{}}, nil
	}

	terms := facetResult.Terms.Terms()
	rows := make([]AggregationRow, 0, len(terms))

	for _, term := range terms {
		// Build a query scoped to this term.
		termQuery := bleve.NewTermQuery(term.Term)
		termQuery.SetField(gb.Field)

		scopedQuery := bleve.NewConjunctionQuery(baseQuery, termQuery)

		metrics, err := computeMetrics(idx, scopedQuery, specs)
		if err != nil {
			return nil, fmt.Errorf("compute metrics for group %q: %w", term.Term, err)
		}

		rows = append(rows, AggregationRow{
			Group:   map[string]interface{}{gb.Field: term.Term},
			Metrics: metrics,
		})
	}

	return &AggregationResponse{Data: rows}, nil
}

// groupByFixedWidth creates numeric range buckets of equal width.
func (e *Engine) groupByFixedWidth(idx bleve.Index, baseQuery query.Query, gb GroupBySpec, specs []AggregationSpec) (*AggregationResponse, error) {
	if gb.Width == nil {
		return nil, fmt.Errorf("fixedWidth groupBy requires a width")
	}
	width := *gb.Width

	// First, find the min and max values for the field to determine the range.
	minVal, maxVal, err := e.findMinMax(idx, baseQuery, gb.Field)
	if err != nil {
		return nil, fmt.Errorf("find min/max for fixedWidth: %w", err)
	}
	if minVal == nil || maxVal == nil {
		return &AggregationResponse{Data: []AggregationRow{}}, nil
	}

	// Create range facets.
	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = 0
	facet := bleve.NewFacetRequest(gb.Field, 10000)

	start := math.Floor(*minVal/width) * width
	for lo := start; lo <= *maxVal; lo += width {
		loVal := lo
		hiVal := lo + width
		name := fmt.Sprintf("[%.0f,%.0f)", loVal, hiVal)
		facet.AddNumericRange(name, &loVal, &hiVal)
	}
	searchReq.AddFacet("groupby", facet)

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("fixed width facet search: %w", err)
	}

	facetResult, ok := result.Facets["groupby"]
	if !ok {
		return &AggregationResponse{Data: []AggregationRow{}}, nil
	}

	rows := make([]AggregationRow, 0)
	for _, nr := range facetResult.NumericRanges {
		if nr.Count == 0 {
			continue
		}

		// Build a numeric range query scoped to this bucket.
		lo := nr.Min
		hi := nr.Max
		inclusive := true
		exclusive := false
		rangeQuery := bleve.NewNumericRangeInclusiveQuery(lo, hi, &inclusive, &exclusive)
		rangeQuery.SetField(gb.Field)

		scopedQuery := bleve.NewConjunctionQuery(baseQuery, rangeQuery)

		metrics, err := computeMetrics(idx, scopedQuery, specs)
		if err != nil {
			return nil, fmt.Errorf("compute metrics for range %s: %w", nr.Name, err)
		}

		rows = append(rows, AggregationRow{
			Group:   map[string]interface{}{gb.Field: nr.Name},
			Metrics: metrics,
		})
	}

	return &AggregationResponse{Data: rows}, nil
}

// groupByRanges uses user-specified numeric ranges to create buckets.
// Uses Palantir V2 startValue (inclusive) / endValue (exclusive) format.
func (e *Engine) groupByRanges(idx bleve.Index, baseQuery query.Query, gb GroupBySpec, specs []AggregationSpec) (*AggregationResponse, error) {
	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = 0
	facet := bleve.NewFacetRequest(gb.Field, 10000)

	for i, r := range gb.Ranges {
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("range_%d", i)
		}

		// Palantir V2: startValue is inclusive lower bound, endValue is exclusive upper bound
		// Bleve numeric ranges are [lo, hi)
		facet.AddNumericRange(name, r.StartValue, r.EndValue)
	}
	searchReq.AddFacet("groupby", facet)

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("ranges facet search: %w", err)
	}

	facetResult, ok := result.Facets["groupby"]
	if !ok {
		return &AggregationResponse{Data: []AggregationRow{}}, nil
	}

	rows := make([]AggregationRow, 0)
	for _, nr := range facetResult.NumericRanges {
		if nr.Count == 0 {
			continue
		}

		// Build a numeric range query scoped to this bucket.
		lo := nr.Min
		hi := nr.Max
		inclusive := true
		exclusive := false
		var minIncPtr, maxIncPtr *bool
		if lo != nil {
			minIncPtr = &inclusive
		}
		if hi != nil {
			maxIncPtr = &exclusive
		}
		rangeQuery := bleve.NewNumericRangeInclusiveQuery(lo, hi, minIncPtr, maxIncPtr)
		rangeQuery.SetField(gb.Field)

		scopedQuery := bleve.NewConjunctionQuery(baseQuery, rangeQuery)

		metrics, err := computeMetrics(idx, scopedQuery, specs)
		if err != nil {
			return nil, fmt.Errorf("compute metrics for range %s: %w", nr.Name, err)
		}

		rows = append(rows, AggregationRow{
			Group:   map[string]interface{}{gb.Field: nr.Name},
			Metrics: metrics,
		})
	}

	return &AggregationResponse{Data: rows}, nil
}

// parseDuration converts a simple ISO 8601 duration string to a time.Duration.
// Supports P1D, P1W, P1M, P1Y (approximate).
func parseDuration(iso string) (time.Duration, error) {
	switch iso {
	case "P1D":
		return 24 * time.Hour, nil
	case "P1W":
		return 7 * 24 * time.Hour, nil
	case "P1M":
		return 30 * 24 * time.Hour, nil
	case "P1Y":
		return 365 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported duration: %q (supported: P1D, P1W, P1M, P1Y)", iso)
	}
}

// groupByDuration groups timestamp values into duration-based buckets.
func (e *Engine) groupByDuration(idx bleve.Index, baseQuery query.Query, gb GroupBySpec, specs []AggregationSpec) (*AggregationResponse, error) {
	dur, err := parseDuration(gb.Duration)
	if err != nil {
		return nil, fmt.Errorf("parse duration: %w", err)
	}

	// Fetch all documents with the timestamp field.
	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = 10000
	searchReq.Fields = []string{gb.Field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("duration search: %w", err)
	}

	if len(result.Hits) == 0 {
		return &AggregationResponse{Data: []AggregationRow{}}, nil
	}

	// Bucket timestamps by duration. Timestamps may be stored as strings or numeric (epoch).
	durSec := dur.Seconds()
	buckets := make(map[int64][]string) // bucket start epoch -> doc IDs

	for _, hit := range result.Hits {
		val, ok := hit.Fields[gb.Field]
		if !ok {
			continue
		}

		var epoch float64
		switch v := val.(type) {
		case float64:
			epoch = v
		case string:
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				continue
			}
			epoch = float64(t.Unix())
		default:
			continue
		}

		bucketStart := int64(math.Floor(epoch/durSec) * durSec)
		buckets[bucketStart] = append(buckets[bucketStart], hit.ID)
	}

	rows := make([]AggregationRow, 0, len(buckets))
	for bucketStart, docIDs := range buckets {
		// Build a query scoped to documents in this bucket.
		docIDQ := bleve.NewDocIDQuery(docIDs)
		scopedQuery := bleve.NewConjunctionQuery(baseQuery, docIDQ)

		metrics, err := computeMetrics(idx, scopedQuery, specs)
		if err != nil {
			return nil, fmt.Errorf("compute metrics for duration bucket: %w", err)
		}

		startTime := time.Unix(bucketStart, 0).UTC().Format(time.RFC3339)
		rows = append(rows, AggregationRow{
			Group:   map[string]interface{}{gb.Field: startTime},
			Metrics: metrics,
		})
	}

	return &AggregationResponse{Data: rows}, nil
}

// findMinMax finds the minimum and maximum values for a numeric field.
func (e *Engine) findMinMax(idx bleve.Index, baseQuery query.Query, field string) (*float64, *float64, error) {
	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = 10000
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, nil, err
	}

	if len(result.Hits) == 0 {
		return nil, nil, nil
	}

	minVal := math.MaxFloat64
	maxVal := -math.MaxFloat64
	found := false

	for _, hit := range result.Hits {
		val, ok := hit.Fields[field]
		if !ok {
			continue
		}
		numVal, ok := val.(float64)
		if !ok {
			continue
		}
		found = true
		if numVal < minVal {
			minVal = numVal
		}
		if numVal > maxVal {
			maxVal = numVal
		}
	}

	if !found {
		return nil, nil, nil
	}

	return &minVal, &maxVal, nil
}
