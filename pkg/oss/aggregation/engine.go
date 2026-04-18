package aggregation

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// AggregationRequest represents a Palantir V2 aggregation request.
type AggregationRequest struct {
	ObjectType      string               `json:"objectType"`
	Query           *bleve.SearchRequest `json:"-"` // pre-built search request (may be nil for all objects)
	Aggregations    []AggregationSpec    `json:"aggregation"`
	GroupBy         []GroupBySpec        `json:"groupBy,omitempty"`
	SubAggregations []SubAggregationSpec `json:"subAggregations,omitempty"`
	Having          []HavingClause       `json:"having,omitempty"`
}

// SubAggregationSpec is a named child aggregation that runs against the scope
// of each leaf bucket of the parent (or the request scope when the parent has
// no groupBy). Sub-aggregations may themselves carry sub-aggregations to any
// depth — duplicate names within the same level are rejected at validation.
type SubAggregationSpec struct {
	Name            string               `json:"name"`
	Aggregations    []AggregationSpec    `json:"aggregation"`
	GroupBy         []GroupBySpec        `json:"groupBy,omitempty"`
	SubAggregations []SubAggregationSpec `json:"subAggregations,omitempty"`
	Having          []HavingClause       `json:"having,omitempty"`
}

// HavingClause is a post-aggregation row filter. Each clause names a metric
// produced by the same request, a comparison op, and a numeric threshold.
// Rows whose metric value fails any clause are dropped from the response.
// Missing / non-numeric metric values always fail — use a dedicated metric
// name (not a dotted default-derived name) for robust matching.
type HavingClause struct {
	Metric string  `json:"metric"`
	Op     string  `json:"op"` // eq, ne, gt, gte, lt, lte
	Value  float64 `json:"value"`
}

// AggregationSpec defines what to aggregate.
type AggregationSpec struct {
	Type        string    `json:"type"`                  // "count", "min", "max", "sum", "avg", "approximateDistinct", "exactDistinct", "standardDeviation", "variance", "approximatePercentile", "collectList"
	Field       string    `json:"field,omitempty"`       // required for min/max/sum/avg
	Name        string    `json:"name,omitempty"`        // output name
	Percentile  *float64  `json:"percentile,omitempty"`  // for approximatePercentile (0-100), scalar result
	Percentiles []float64 `json:"percentiles,omitempty"` // for approximatePercentile batch: single HdrHistogram pass, map[string]float64 result
	MaxItems    *int      `json:"maxItems,omitempty"`    // for collectList: max values to collect (default 100)
	Precision   *int      `json:"precision,omitempty"`   // for approximateDistinct: HyperLogLog precision, 4..18 (default 14 — ~0.81% standard error)
}

// GroupBySpec defines how to group results.
type GroupBySpec struct {
	Type          string         `json:"type"` // "exact", "fixedWidth", "range", "ranges", "duration", "topValues", "geohash"
	Field         string         `json:"field"`
	MaxGroups     *int           `json:"maxGroupCount,omitempty"`
	Width         *float64       `json:"fixedWidth,omitempty"` // for fixedWidth
	Ranges        []Range        `json:"ranges,omitempty"`     // for range/ranges
	Duration      string         `json:"duration,omitempty"`   // ISO 8601: P1D, P1W, P1M, P1Y
	DurationValue *DurationValue `json:"value,omitempty"`      // for duration: {unit: "DAYS", value: 30}
	Precision     *int           `json:"precision,omitempty"`  // for geohash: character precision 1..12 (default 6 ≈ ±0.61km)
}

// DurationValue represents a duration using Palantir V2 unit/value format.
type DurationValue struct {
	Unit  string  `json:"unit"`  // "DAYS", "WEEKS", "MONTHS", "YEARS", "HOURS", "MINUTES", "SECONDS"
	Value float64 `json:"value"` // number of units
}

// Range defines a range bucket using Palantir V2 startValue/endValue format.
type Range struct {
	Name       string   `json:"name,omitempty"`
	StartValue *float64 `json:"startValue,omitempty"` // inclusive
	EndValue   *float64 `json:"endValue,omitempty"`   // exclusive
}

// Engine computes aggregations.
type Engine struct {
	// MaxDocScanSize is the maximum number of documents fetched in a single scan.
	// When results are truncated, accuracy is marked as APPROXIMATE.
	MaxDocScanSize int
}

// NewEngine creates a new aggregation engine.
func NewEngine() *Engine {
	return &Engine{MaxDocScanSize: 10000}
}

// Aggregate performs aggregation on the given index.
func (e *Engine) Aggregate(idx bleve.Index, req *AggregationRequest) (*AggregationResponse, error) {
	return e.AggregateWithQuery(idx, nil, req)
}

// AggregateWithQuery performs aggregation on the given index with an explicit base query.
func (e *Engine) AggregateWithQuery(idx bleve.Index, baseQuery query.Query, req *AggregationRequest) (*AggregationResponse, error) {
	if baseQuery == nil {
		baseQuery = bleve.NewMatchAllQuery()
	}

	if err := validateSubAggregations(req.SubAggregations); err != nil {
		return nil, err
	}
	if err := ValidateHaving(req.Having); err != nil {
		return nil, err
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

	if len(req.SubAggregations) > 0 && len(req.GroupBy) == 0 {
		subs, subTrunc, err := e.runSubAggregations(idx, baseQuery, req.SubAggregations)
		if err != nil {
			return nil, err
		}
		resp.SubAggregations = subs
		if subTrunc {
			resp.Accuracy = "APPROXIMATE"
		}
	}

	if len(req.Having) > 0 {
		resp.Data = ApplyHaving(resp.Data, req.Having)
	}

	if resp.Accuracy == "" {
		resp.Accuracy = "ACCURATE"
	}
	resp.ComputeUsage = 4.0
	return resp, nil
}

// validateSubAggregations enforces non-empty Names and uniqueness within a
// single level, recursing into nested sub-aggregations.
func validateSubAggregations(subs []SubAggregationSpec) error {
	if len(subs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(subs))
	for i, s := range subs {
		if s.Name == "" {
			return fmt.Errorf("subAggregations[%d]: name is required", i)
		}
		if _, dup := seen[s.Name]; dup {
			return fmt.Errorf("subAggregations[%d]: duplicate name %q", i, s.Name)
		}
		seen[s.Name] = struct{}{}
		if err := validateSubAggregations(s.SubAggregations); err != nil {
			return fmt.Errorf("subAggregations[%d] (%s): %w", i, s.Name, err)
		}
	}
	return nil
}

// runSubAggregations executes each named sub-aggregation against the given
// scope query and returns the results keyed by Name. Each sub-aggregation
// reuses Aggregate's grouping + recursion machinery so nested sub-aggregations
// resolve transparently.
func (e *Engine) runSubAggregations(idx bleve.Index, scope query.Query, subs []SubAggregationSpec) (map[string]*AggregationResponse, bool, error) {
	if len(subs) == 0 {
		return nil, false, nil
	}
	out := make(map[string]*AggregationResponse, len(subs))
	var truncated bool
	for _, s := range subs {
		childReq := &AggregationRequest{
			Aggregations:    s.Aggregations,
			GroupBy:         s.GroupBy,
			SubAggregations: s.SubAggregations,
			Having:          s.Having,
		}
		childResp, err := e.AggregateWithQuery(idx, scope, childReq)
		if err != nil {
			return nil, false, fmt.Errorf("subAggregation %q: %w", s.Name, err)
		}
		if childResp.Accuracy == "APPROXIMATE" {
			truncated = true
		}
		out[s.Name] = childResp
	}
	return out, truncated, nil
}

// aggregateSimple performs aggregation without groupBy.
func (e *Engine) aggregateSimple(idx bleve.Index, baseQuery query.Query, req *AggregationRequest) (*AggregationResponse, error) {
	metrics, truncated, err := e.computeMetrics(idx, baseQuery, req.Aggregations)
	if err != nil {
		return nil, fmt.Errorf("compute metrics: %w", err)
	}

	resp := &AggregationResponse{
		Data: []AggregationRow{
			{Metrics: metrics},
		},
	}
	if truncated {
		resp.Accuracy = "APPROXIMATE"
	}
	return resp, nil
}

// groupEntry pairs a group key value with its scoped Bleve query.
type groupEntry struct {
	value      interface{}
	scopeQuery query.Query
}

// sortGroupEntries orders buckets within a single groupBy layer so response
// rows are deterministic: non-null values sort ascending by stringified key
// (alphabetical for exact/topValues, lexicographic for fixedWidth bucket
// names like "[0,100)", and RFC3339 — which is chronological — for duration
// buckets). Nil values (null-group) are placed last.
func sortGroupEntries(entries []groupEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		vi, vj := entries[i].value, entries[j].value
		iNil := vi == nil
		jNil := vj == nil
		if iNil && jNil {
			return false
		}
		if iNil {
			return false
		}
		if jNil {
			return true
		}
		return fmt.Sprint(vi) < fmt.Sprint(vj)
	})
}

// aggregateWithGroupBy performs aggregation with groupBy using Bleve facets.
// Supports multiple groupBy specs by recursive nesting.
func (e *Engine) aggregateWithGroupBy(idx bleve.Index, baseQuery query.Query, req *AggregationRequest) (*AggregationResponse, error) {
	if len(req.GroupBy) == 0 {
		return nil, fmt.Errorf("groupBy is empty")
	}

	rows, truncated, err := e.recursiveGroupBy(idx, baseQuery, req.GroupBy, req.Aggregations, req.SubAggregations)
	if err != nil {
		return nil, err
	}

	resp := &AggregationResponse{Data: rows}
	if truncated {
		resp.Accuracy = "APPROXIMATE"
	}
	return resp, nil
}

// recursiveGroupBy processes groupBy specs one at a time, nesting results.
// Sub-aggregations attach to leaf rows only; intermediate groupBy levels
// pass them through unchanged.
func (e *Engine) recursiveGroupBy(idx bleve.Index, baseQuery query.Query, groupBys []GroupBySpec, specs []AggregationSpec, subs []SubAggregationSpec) ([]AggregationRow, bool, error) {
	gb := groupBys[0]
	remaining := groupBys[1:]

	entries, truncated, err := e.getGroupEntries(idx, baseQuery, gb)
	if err != nil {
		return nil, false, err
	}

	sortGroupEntries(entries)

	var rows []AggregationRow
	for _, entry := range entries {
		if len(remaining) > 0 {
			// Recurse with narrowed scope
			subRows, subTrunc, err := e.recursiveGroupBy(idx, entry.scopeQuery, remaining, specs, subs)
			if err != nil {
				return nil, false, err
			}
			if subTrunc {
				truncated = true
			}
			for _, subRow := range subRows {
				combined := make(map[string]interface{})
				combined[gb.Field] = entry.value
				for k, v := range subRow.Group {
					combined[k] = v
				}
				rows = append(rows, AggregationRow{
					Group:           combined,
					Metrics:         subRow.Metrics,
					SubAggregations: subRow.SubAggregations,
				})
			}
		} else {
			// Leaf level — compute metrics + run sub-aggregations against bucket scope.
			metrics, leafTrunc, err := e.computeMetrics(idx, entry.scopeQuery, specs)
			if err != nil {
				return nil, false, fmt.Errorf("compute metrics for group %v: %w", entry.value, err)
			}
			if leafTrunc {
				truncated = true
			}
			row := AggregationRow{
				Group:   map[string]interface{}{gb.Field: entry.value},
				Metrics: metrics,
			}
			if len(subs) > 0 {
				subResults, subTrunc, err := e.runSubAggregations(idx, entry.scopeQuery, subs)
				if err != nil {
					return nil, false, fmt.Errorf("sub-aggregations for group %v: %w", entry.value, err)
				}
				if subTrunc {
					truncated = true
				}
				row.SubAggregations = subResults
			}
			rows = append(rows, row)
		}
	}

	return rows, truncated, nil
}

// getGroupEntries dispatches to the appropriate groupBy type and returns scoped entries.
func (e *Engine) getGroupEntries(idx bleve.Index, baseQuery query.Query, gb GroupBySpec) ([]groupEntry, bool, error) {
	switch gb.Type {
	case "exact":
		return e.getExactEntries(idx, baseQuery, gb)
	case "fixedWidth":
		return e.getFixedWidthEntries(idx, baseQuery, gb)
	case "range", "ranges":
		return e.getRangesEntries(idx, baseQuery, gb)
	case "duration":
		return e.getDurationEntries(idx, baseQuery, gb)
	case "topValues":
		return e.getTopValuesEntries(idx, baseQuery, gb)
	case "geohash":
		return e.getGeohashEntries(idx, baseQuery, gb)
	default:
		return nil, false, fmt.Errorf("unsupported groupBy type: %q", gb.Type)
	}
}

// getExactEntries returns group entries for exact term grouping.
// When the facet reports a non-zero Missing count, a null-group entry is
// appended with value=nil and a scope query that excludes every observed
// term so nested recursion still resolves correct per-bucket metrics.
func (e *Engine) getExactEntries(idx bleve.Index, baseQuery query.Query, gb GroupBySpec) ([]groupEntry, bool, error) {
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
		return nil, false, fmt.Errorf("facet search: %w", err)
	}

	facetResult, ok := result.Facets["groupby"]
	if !ok || facetResult.Terms == nil {
		return nil, false, nil
	}

	terms := facetResult.Terms.Terms()
	entries := make([]groupEntry, 0, len(terms)+1)
	termQueries := make([]query.Query, 0, len(terms))
	for _, term := range terms {
		termQ := bleve.NewTermQuery(term.Term)
		termQ.SetField(gb.Field)
		termQueries = append(termQueries, termQ)
		entries = append(entries, groupEntry{
			value:      term.Term,
			scopeQuery: bleve.NewConjunctionQuery(baseQuery, termQ),
		})
	}

	// Only surface a null bucket when at least one real term was observed.
	// Grouping by a non-existent field (all docs "missing") returns no
	// groups — matches pre-existing Palantir behaviour.
	if facetResult.Missing > 0 && len(termQueries) > 0 {
		nullScope := bleve.NewBooleanQuery()
		nullScope.AddMust(baseQuery)
		for _, tq := range termQueries {
			nullScope.AddMustNot(tq)
		}
		entries = append(entries, groupEntry{
			value:      nil,
			scopeQuery: nullScope,
		})
	}

	return entries, false, nil
}

// getTopValuesEntries returns group entries for top N term grouping.
func (e *Engine) getTopValuesEntries(idx bleve.Index, baseQuery query.Query, gb GroupBySpec) ([]groupEntry, bool, error) {
	maxGroups := 10
	if gb.MaxGroups != nil {
		maxGroups = *gb.MaxGroups
	}

	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = 0
	facet := bleve.NewFacetRequest(gb.Field, maxGroups)
	searchReq.AddFacet("topvalues", facet)

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, false, fmt.Errorf("topValues facet search: %w", err)
	}

	facetResult, ok := result.Facets["topvalues"]
	if !ok || facetResult.Terms == nil {
		return nil, false, nil
	}

	terms := facetResult.Terms.Terms()
	entries := make([]groupEntry, 0, len(terms))
	for _, term := range terms {
		termQ := bleve.NewTermQuery(term.Term)
		termQ.SetField(gb.Field)
		entries = append(entries, groupEntry{
			value:      term.Term,
			scopeQuery: bleve.NewConjunctionQuery(baseQuery, termQ),
		})
	}
	return entries, false, nil
}

// getFixedWidthEntries returns group entries for fixed-width numeric range grouping.
func (e *Engine) getFixedWidthEntries(idx bleve.Index, baseQuery query.Query, gb GroupBySpec) ([]groupEntry, bool, error) {
	if gb.Width == nil {
		return nil, false, fmt.Errorf("fixedWidth groupBy requires a width")
	}
	width := *gb.Width

	minVal, maxVal, truncated, err := e.findMinMax(idx, baseQuery, gb.Field)
	if err != nil {
		return nil, false, fmt.Errorf("find min/max for fixedWidth: %w", err)
	}
	if minVal == nil || maxVal == nil {
		return nil, false, nil
	}

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
		return nil, false, fmt.Errorf("fixed width facet search: %w", err)
	}

	facetResult, ok := result.Facets["groupby"]
	if !ok {
		return nil, truncated, nil
	}

	entries := make([]groupEntry, 0)
	for _, nr := range facetResult.NumericRanges {
		if nr.Count == 0 {
			continue
		}
		lo := nr.Min
		hi := nr.Max
		inclusive := true
		exclusive := false
		rangeQuery := bleve.NewNumericRangeInclusiveQuery(lo, hi, &inclusive, &exclusive)
		rangeQuery.SetField(gb.Field)
		entries = append(entries, groupEntry{
			value:      nr.Name,
			scopeQuery: bleve.NewConjunctionQuery(baseQuery, rangeQuery),
		})
	}
	return entries, truncated, nil
}

// getRangesEntries returns group entries for user-specified numeric range grouping.
func (e *Engine) getRangesEntries(idx bleve.Index, baseQuery query.Query, gb GroupBySpec) ([]groupEntry, bool, error) {
	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = 0
	facet := bleve.NewFacetRequest(gb.Field, 10000)

	for i, r := range gb.Ranges {
		name := r.Name
		if name == "" {
			name = fmt.Sprintf("range_%d", i)
		}
		facet.AddNumericRange(name, r.StartValue, r.EndValue)
	}
	searchReq.AddFacet("groupby", facet)

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, false, fmt.Errorf("ranges facet search: %w", err)
	}

	facetResult, ok := result.Facets["groupby"]
	if !ok {
		return nil, false, nil
	}

	entries := make([]groupEntry, 0)
	for _, nr := range facetResult.NumericRanges {
		if nr.Count == 0 {
			continue
		}
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
		entries = append(entries, groupEntry{
			value:      nr.Name,
			scopeQuery: bleve.NewConjunctionQuery(baseQuery, rangeQuery),
		})
	}
	return entries, false, nil
}

// getDurationEntries returns group entries for duration-based timestamp grouping.
func (e *Engine) getDurationEntries(idx bleve.Index, baseQuery query.Query, gb GroupBySpec) ([]groupEntry, bool, error) {
	var durSec float64

	switch {
	case gb.DurationValue != nil:
		secs, err := durationValueToSeconds(gb.DurationValue)
		if err != nil {
			return nil, false, fmt.Errorf("parse duration value: %w", err)
		}
		durSec = secs
	case gb.Duration != "":
		dur, err := parseDuration(gb.Duration)
		if err != nil {
			return nil, false, fmt.Errorf("parse duration: %w", err)
		}
		durSec = dur.Seconds()
	default:
		return nil, false, fmt.Errorf("duration groupBy requires either 'duration' (ISO 8601) or 'value' ({unit, value})")
	}

	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = e.MaxDocScanSize
	searchReq.Fields = []string{gb.Field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, false, fmt.Errorf("duration search: %w", err)
	}

	if len(result.Hits) == 0 {
		return nil, false, nil
	}

	truncated := result.Total > uint64(len(result.Hits))

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

	entries := make([]groupEntry, 0, len(buckets))
	for bucketStart, docIDs := range buckets {
		docIDQ := bleve.NewDocIDQuery(docIDs)
		scopedQuery := bleve.NewConjunctionQuery(baseQuery, docIDQ)
		startTime := time.Unix(bucketStart, 0).UTC().Format(time.RFC3339)
		entries = append(entries, groupEntry{
			value:      startTime,
			scopeQuery: scopedQuery,
		})
	}
	return entries, truncated, nil
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

		metrics, _, err := e.computeMetrics(idx, scopedQuery, specs)
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
	minVal, maxVal, truncated, err := e.findMinMax(idx, baseQuery, gb.Field)
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

		metrics, _, err := e.computeMetrics(idx, scopedQuery, specs)
		if err != nil {
			return nil, fmt.Errorf("compute metrics for range %s: %w", nr.Name, err)
		}

		rows = append(rows, AggregationRow{
			Group:   map[string]interface{}{gb.Field: nr.Name},
			Metrics: metrics,
		})
	}

	resp := &AggregationResponse{Data: rows}
	if truncated {
		resp.Accuracy = "APPROXIMATE"
	}
	return resp, nil
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

		metrics, _, err := e.computeMetrics(idx, scopedQuery, specs)
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

// durationValueToSeconds converts a DurationValue to seconds.
func durationValueToSeconds(dv *DurationValue) (float64, error) {
	switch dv.Unit {
	case "SECONDS":
		return dv.Value, nil
	case "MINUTES":
		return dv.Value * 60, nil
	case "HOURS":
		return dv.Value * 3600, nil
	case "DAYS":
		return dv.Value * 86400, nil
	case "WEEKS":
		return dv.Value * 7 * 86400, nil
	case "MONTHS":
		return dv.Value * 30 * 86400, nil
	case "YEARS":
		return dv.Value * 365 * 86400, nil
	default:
		return 0, fmt.Errorf("unsupported duration unit: %q (supported: SECONDS, MINUTES, HOURS, DAYS, WEEKS, MONTHS, YEARS)", dv.Unit)
	}
}

// groupByDuration groups timestamp values into duration-based buckets.
func (e *Engine) groupByDuration(idx bleve.Index, baseQuery query.Query, gb GroupBySpec, specs []AggregationSpec) (*AggregationResponse, error) {
	var durSec float64

	switch {
	case gb.DurationValue != nil:
		// Palantir V2 unit/value format: {"unit": "DAYS", "value": 30}
		secs, err := durationValueToSeconds(gb.DurationValue)
		if err != nil {
			return nil, fmt.Errorf("parse duration value: %w", err)
		}
		durSec = secs
	case gb.Duration != "":
		// ISO 8601 format: P1D, P1W, P1M, P1Y
		dur, err := parseDuration(gb.Duration)
		if err != nil {
			return nil, fmt.Errorf("parse duration: %w", err)
		}
		durSec = dur.Seconds()
	default:
		return nil, fmt.Errorf("duration groupBy requires either 'duration' (ISO 8601) or 'value' ({unit, value})")
	}

	// Fetch all documents with the timestamp field.
	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = e.MaxDocScanSize
	searchReq.Fields = []string{gb.Field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("duration search: %w", err)
	}

	if len(result.Hits) == 0 {
		return &AggregationResponse{Data: []AggregationRow{}}, nil
	}

	truncated := result.Total > uint64(len(result.Hits))

	// Bucket timestamps by duration. Timestamps may be stored as strings or numeric (epoch).
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

		metrics, _, err := e.computeMetrics(idx, scopedQuery, specs)
		if err != nil {
			return nil, fmt.Errorf("compute metrics for duration bucket: %w", err)
		}

		startTime := time.Unix(bucketStart, 0).UTC().Format(time.RFC3339)
		rows = append(rows, AggregationRow{
			Group:   map[string]interface{}{gb.Field: startTime},
			Metrics: metrics,
		})
	}

	resp := &AggregationResponse{Data: rows}
	if truncated {
		resp.Accuracy = "APPROXIMATE"
	}
	return resp, nil
}

// groupByTopValues groups by the top N most frequent values for a field.
// It uses a term facet sorted by count descending and returns the top maxGroupCount groups.
func (e *Engine) groupByTopValues(idx bleve.Index, baseQuery query.Query, gb GroupBySpec, specs []AggregationSpec) (*AggregationResponse, error) {
	maxGroups := 10
	if gb.MaxGroups != nil {
		maxGroups = *gb.MaxGroups
	}

	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = 0
	facet := bleve.NewFacetRequest(gb.Field, maxGroups)
	searchReq.AddFacet("topvalues", facet)

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("topValues facet search: %w", err)
	}

	facetResult, ok := result.Facets["topvalues"]
	if !ok {
		return &AggregationResponse{Data: []AggregationRow{}}, nil
	}

	if facetResult.Terms == nil {
		return &AggregationResponse{Data: []AggregationRow{}}, nil
	}

	// Bleve returns terms sorted by count descending already.
	terms := facetResult.Terms.Terms()
	rows := make([]AggregationRow, 0, len(terms))

	for _, term := range terms {
		termQuery := bleve.NewTermQuery(term.Term)
		termQuery.SetField(gb.Field)

		scopedQuery := bleve.NewConjunctionQuery(baseQuery, termQuery)

		metrics, _, err := e.computeMetrics(idx, scopedQuery, specs)
		if err != nil {
			return nil, fmt.Errorf("compute metrics for topValues group %q: %w", term.Term, err)
		}

		rows = append(rows, AggregationRow{
			Group:   map[string]interface{}{gb.Field: term.Term},
			Metrics: metrics,
		})
	}

	return &AggregationResponse{Data: rows}, nil
}

// findMinMax finds the minimum and maximum values for a numeric field.
// Returns (min, max, truncated, error). truncated is true when total docs exceed scan size.
func (e *Engine) findMinMax(idx bleve.Index, baseQuery query.Query, field string) (*float64, *float64, bool, error) {
	searchReq := bleve.NewSearchRequest(baseQuery)
	searchReq.Size = e.MaxDocScanSize
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, nil, false, err
	}

	if len(result.Hits) == 0 {
		return nil, nil, false, nil
	}

	truncated := result.Total > uint64(len(result.Hits))

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
		return nil, nil, false, nil
	}

	return &minVal, &maxVal, truncated, nil
}
