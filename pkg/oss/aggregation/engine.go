package aggregation

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/liyang/weave/pkg/oss/where"
)

// AggregationRequest represents a Palantir V2 aggregation request.
type AggregationRequest struct {
	ObjectType      string               `json:"objectType"`
	Query           *bleve.SearchRequest `json:"-"` // pre-built search request (may be nil for all objects)
	Where           *where.WhereClause   `json:"where,omitempty"`
	Aggregations    []AggregationSpec    `json:"aggregation"`
	GroupBy         []GroupBySpec        `json:"groupBy,omitempty"`
	SubAggregations []SubAggregationSpec `json:"subAggregations,omitempty"`
	Having          []HavingClause       `json:"having,omitempty"`
	// Cube, when true, computes every 2^N subset of the declared groupBys and
	// concatenates the resulting rows (most specific subset first, then every
	// (N-1)-subset, down to the grand total). Non-grouped dimensions in a row
	// are marked absent from Group — callers detect aggregated dimensions by
	// key absence.
	Cube bool `json:"cube,omitempty"`
	// Rollup, when true, computes the hierarchical chain [gb[0..N]], [gb[0..N-1]],
	// ..., [gb[0..0]], [] — N+1 result groupings in total. Same Group-absence
	// semantics as Cube for rolled-up dimensions. Mutex with Cube; if both are
	// set Cube wins.
	Rollup bool `json:"rollup,omitempty"`
	// Accuracy is the Palantir-style request-level toggle that lets callers
	// opt out of the engine's default approximate algorithms (HyperLogLog
	// for approximateDistinct, t-digest for approximatePercentile).
	// Two values are recognised:
	//   - "" or "ALLOW_APPROXIMATE" (default): approximate aggregations run
	//     their sketches; the response.accuracy field reports whether the
	//     produced result was actually approximate at runtime.
	//   - "REQUIRE_ACCURATE": approximate aggregations are transparently
	//     promoted to their exact counterparts (approximateDistinct →
	//     exactDistinct, approximatePercentile → sort-based percentile),
	//     so existing specs can demand byte-exact output without a rewrite.
	Accuracy string `json:"accuracy,omitempty"`
	// ExcludedItems is an optional list of primary keys to exclude from the
	// aggregation scope BEFORE any metric or groupBy facet runs (US-382).
	// Duplicates are tolerated; PKs that don't match the post-scope query
	// contribute zero to the response.excludedItems counter. Implemented as
	// a Bleve Boolean MUST_NOT wrap of the base query so the exclusion is
	// uniformly visible to every downstream code path (groupBy, sub-aggs,
	// derived-field path).
	ExcludedItems []string `json:"excludedItems,omitempty"`
	// HLLPrecision is the request-wide HyperLogLog precision used by every
	// approximateDistinct in this request that does not carry a per-spec
	// Precision override (US-465). Range: 4..18, default 14. Out-of-range
	// values are rejected at AggregateWithQuery time so callers get a clean
	// 400 instead of a panic from inside the sketch.
	HLLPrecision *int `json:"hllPrecision,omitempty"`
	// TDigestCompression is the request-wide t-digest compression used by
	// every approximatePercentile in this request that does not carry a
	// per-spec Compression override (US-465). Default 100. Must be a
	// positive finite float — negative, zero, NaN, or +Inf are rejected at
	// validation time.
	TDigestCompression *float64 `json:"tdigestCompression,omitempty"`
	// ApproximateScanThreshold is the per-request override of the scanned-
	// row count above which the response is forced to APPROXIMATE accuracy
	// (US-465). nil falls back to DefaultApproximateScanThreshold (1M).
	// The check fires AFTER the engine knows the scoped baseQuery total,
	// so it is independent of whether MaxDocScanSize truncated the scan.
	// A non-positive override disables the threshold entirely.
	ApproximateScanThreshold *int64 `json:"approximateScanThreshold,omitempty"`
}

// Accuracy mode constants for AggregationRequest.Accuracy. Match the Palantir
// V2 wire format. A blank Accuracy is treated as AccuracyAllowApproximate so
// existing callers that never set the field keep their previous semantics.
const (
	AccuracyAllowApproximate = "ALLOW_APPROXIMATE"
	AccuracyRequireAccurate  = "REQUIRE_ACCURATE"
)

// DefaultApproximateScanThreshold is the per-request scanned-row count above
// which an aggregation response is force-marked APPROXIMATE even when the
// scan was not truncated by MaxDocScanSize (US-465). 1M matches the PRD
// "扫描行数阈值默认 1M" acceptance and lines up with the cardinality budget
// the HLL/t-digest accuracy gates exercise.
const DefaultApproximateScanThreshold int64 = 1_000_000

// requireAccurate reports whether the caller demanded exact algorithms for
// approximate-by-default aggregations. The engine forwards this verdict to
// computeMetrics so each leaf bucket inherits the request-level mode.
func requireAccurate(mode string) bool {
	return mode == AccuracyRequireAccurate
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
	Percentiles []float64 `json:"percentiles,omitempty"` // for approximatePercentile batch: single t-digest pass, map[string]float64 result
	MaxItems    *int      `json:"maxItems,omitempty"`    // for collectList: max values to collect (default 100)
	Precision   *int      `json:"precision,omitempty"`   // for approximateDistinct: HyperLogLog precision, 4..18 (default 14 — ~0.81% standard error). Overrides request-level HLLPrecision.
	Compression *float64  `json:"compression,omitempty"` // for approximatePercentile: t-digest compression (default 100). Overrides request-level TDigestCompression. Must be positive finite.
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

// MaxGroupByDepth caps how many groupBy layers a single aggregation request
// may declare. Each layer fans out per-bucket facet/scan work, so deeply
// nested requests can quickly overwhelm a single Bleve index. Tests may
// override via a Cleanup-restored swap. The default of 8 matches the
// per-request limit Palantir V2 documents.
var MaxGroupByDepth = 8

// MaxSubAggregationDepth caps recursive sub-aggregation nesting. Each level
// runs an additional per-bucket aggregation pass; an unbounded spec can
// blow the heap on a small index. The default of 8 is generous for any
// real-world dashboard while still rejecting pathological / hand-rolled
// adversarial specs.
var MaxSubAggregationDepth = 8

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
	startTime := time.Now()

	if baseQuery == nil {
		baseQuery = bleve.NewMatchAllQuery()
	}
	if req.Where != nil {
		whereQuery, err := where.ConvertToBleveQuery(req.Where)
		if err != nil {
			return nil, fmt.Errorf("where: %w", err)
		}
		baseQuery = bleve.NewConjunctionQuery(baseQuery, whereQuery)
	}

	if len(req.GroupBy) > MaxGroupByDepth {
		return nil, fmt.Errorf("groupBy depth %d exceeds limit %d", len(req.GroupBy), MaxGroupByDepth)
	}
	if err := validateSubAggregations(req.SubAggregations); err != nil {
		return nil, err
	}
	if err := ValidateHaving(req.Having); err != nil {
		return nil, err
	}
	if err := validateApproxConfig(req); err != nil {
		return nil, err
	}

	// US-382: pre-filter excludedItems BEFORE any metric or facet runs. We
	// count the actual intersection (caller-requested PKs that resolve in
	// the base scope) so the response.excludedItems field reports what the
	// engine truly removed — duplicate or out-of-scope PKs contribute zero.
	excludedCount, scopedQuery, err := applyExcludedItems(idx, baseQuery, req.ExcludedItems)
	if err != nil {
		return nil, err
	}
	baseQuery = scopedQuery

	var resp *AggregationResponse

	cfg := resolveApproxConfig(req)

	switch {
	case (req.Cube || req.Rollup) && len(req.GroupBy) > 0:
		resp, err = e.aggregateCubeOrRollup(idx, baseQuery, req, cfg)
	case len(req.GroupBy) > 0:
		// If groupBy is specified, use Bleve facets.
		resp, err = e.aggregateWithGroupBy(idx, baseQuery, req, cfg)
	default:
		// Simple aggregation without groupBy.
		resp, err = e.aggregateSimple(idx, baseQuery, req, cfg)
	}
	if err != nil {
		return nil, err
	}

	if len(req.SubAggregations) > 0 && len(req.GroupBy) == 0 {
		subs, subTrunc, err := e.runSubAggregations(idx, baseQuery, req.SubAggregations, req.Accuracy, req)
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

	scannedRows := countScannedRows(idx, baseQuery)
	if exceedsApproximateScanThreshold(scannedRows, req.ApproximateScanThreshold) {
		resp.Accuracy = "APPROXIMATE"
	}
	if resp.Accuracy == "" {
		resp.Accuracy = "ACCURATE"
	}
	resp.ExcludedItems = excludedCount
	resp.ComputeUsage = &ComputeUsage{
		ScannedRows: scannedRows,
		DurationMs:  time.Since(startTime).Milliseconds(),
		Accuracy:    resp.Accuracy,
	}
	return resp, nil
}

// validateApproxConfig range-checks the request-level approximate-algorithm
// knobs (hllPrecision, tdigestCompression). It runs before any spec-level
// override is consulted so a single source of truth governs both paths.
// Per-spec Precision is re-validated inside computeMetrics for clarity.
func validateApproxConfig(req *AggregationRequest) error {
	if req.HLLPrecision != nil {
		p := *req.HLLPrecision
		if p < MinHLLPrecision || p > MaxHLLPrecision {
			return fmt.Errorf("hllPrecision %d out of range [%d,%d]", p, MinHLLPrecision, MaxHLLPrecision)
		}
	}
	if req.TDigestCompression != nil {
		c := *req.TDigestCompression
		if !(c > 0) || math.IsInf(c, 0) || math.IsNaN(c) {
			return fmt.Errorf("tdigestCompression %v must be a positive finite number", c)
		}
	}
	return nil
}

// exceedsApproximateScanThreshold reports whether the scanned-row count has
// crossed the configured threshold. A nil override picks the package default
// (1M); a non-positive override disables the check. Centralised so the
// behaviour stays uniform between the simple, grouped, and cube/rollup paths.
func exceedsApproximateScanThreshold(scannedRows int64, override *int64) bool {
	threshold := DefaultApproximateScanThreshold
	if override != nil {
		threshold = *override
	}
	if threshold <= 0 {
		return false
	}
	return scannedRows > threshold
}

// applyExcludedItems wraps baseQuery so that any document whose ID matches one
// of excluded is filtered out before any metric or facet runs. It also reports
// the count of caller-requested PKs that would have matched the base scope —
// this is the value surfaced as response.excludedItems. A nil/empty exclusion
// list is a no-op that returns (0, baseQuery, nil).
//
// We do an explicit intersection count instead of trusting len(excluded) so a
// caller passing duplicates, blank strings, or out-of-scope PKs does not
// inflate the response. The intersection count is also robust under a
// post-Boolean Bleve plan that may rewrite the exclusion query.
func applyExcludedItems(idx bleve.Index, baseQuery query.Query, excluded []string) (int, query.Query, error) {
	if len(excluded) == 0 {
		return 0, baseQuery, nil
	}
	cleaned := make([]string, 0, len(excluded))
	seen := make(map[string]struct{}, len(excluded))
	for _, pk := range excluded {
		if pk == "" {
			continue
		}
		if _, dup := seen[pk]; dup {
			continue
		}
		seen[pk] = struct{}{}
		cleaned = append(cleaned, pk)
	}
	if len(cleaned) == 0 {
		return 0, baseQuery, nil
	}

	docIDQ := bleve.NewDocIDQuery(cleaned)
	intersect := bleve.NewConjunctionQuery(baseQuery, docIDQ)
	countReq := bleve.NewSearchRequest(intersect)
	countReq.Size = 0
	intRes, err := idx.Search(countReq)
	if err != nil {
		return 0, baseQuery, fmt.Errorf("excludedItems intersection count: %w", err)
	}
	excludedCount := int(intRes.Total)

	wrapped := bleve.NewBooleanQuery()
	wrapped.AddMust(baseQuery)
	wrapped.AddMustNot(bleve.NewDocIDQuery(cleaned))
	return excludedCount, wrapped, nil
}

// countScannedRows reports the post-exclusion total document count visible to
// the aggregation engine. This is a lower bound on actual I/O (per-bucket
// facet scans run on top of the same set) but matches what callers want from
// the computeUsage envelope: "how many rows did my query touch?"
//
// Errors are swallowed to a zero count — the surrounding aggregation has
// already produced data; we should not fail the response on a metering search.
func countScannedRows(idx bleve.Index, baseQuery query.Query) int64 {
	countReq := bleve.NewSearchRequest(baseQuery)
	countReq.Size = 0
	res, err := idx.Search(countReq)
	if err != nil {
		return 0
	}
	return int64(res.Total)
}

// validateSubAggregations enforces non-empty Names and uniqueness within a
// single level, recursing into nested sub-aggregations. It also caps the
// recursion depth at MaxSubAggregationDepth so a runaway spec can't blow
// the heap.
func validateSubAggregations(subs []SubAggregationSpec) error {
	return validateSubAggregationsAtDepth(subs, 1)
}

func validateSubAggregationsAtDepth(subs []SubAggregationSpec, depth int) error {
	if len(subs) == 0 {
		return nil
	}
	if depth > MaxSubAggregationDepth {
		return fmt.Errorf("subAggregations depth %d exceeds limit %d", depth, MaxSubAggregationDepth)
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
		if err := validateSubAggregationsAtDepth(s.SubAggregations, depth+1); err != nil {
			return fmt.Errorf("subAggregations[%d] (%s): %w", i, s.Name, err)
		}
	}
	return nil
}

// runSubAggregations executes each named sub-aggregation against the given
// scope query and returns the results keyed by Name. Each sub-aggregation
// reuses Aggregate's grouping + recursion machinery so nested sub-aggregations
// resolve transparently. accuracyMode is propagated unchanged so children
// inherit the request-level REQUIRE_ACCURATE / ALLOW_APPROXIMATE toggle. The
// parentReq pointer carries the request-level HLLPrecision / TDigestCompression
// / ApproximateScanThreshold defaults (US-465) so a sub-aggregation inherits
// the same approximate-algorithm contract as its parent unless it sets
// per-spec overrides.
func (e *Engine) runSubAggregations(idx bleve.Index, scope query.Query, subs []SubAggregationSpec, accuracyMode string, parentReq *AggregationRequest) (map[string]*AggregationResponse, bool, error) {
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
			Accuracy:        accuracyMode,
		}
		if parentReq != nil {
			childReq.HLLPrecision = parentReq.HLLPrecision
			childReq.TDigestCompression = parentReq.TDigestCompression
			childReq.ApproximateScanThreshold = parentReq.ApproximateScanThreshold
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
func (e *Engine) aggregateSimple(idx bleve.Index, baseQuery query.Query, req *AggregationRequest, cfg approxConfig) (*AggregationResponse, error) {
	metrics, truncated, approximate, err := e.computeMetrics(idx, baseQuery, req.Aggregations, req.Accuracy, cfg)
	if err != nil {
		return nil, fmt.Errorf("compute metrics: %w", err)
	}

	resp := &AggregationResponse{
		Data: []AggregationRow{
			{Metrics: metrics},
		},
	}
	if truncated || approximate {
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
func (e *Engine) aggregateWithGroupBy(idx bleve.Index, baseQuery query.Query, req *AggregationRequest, cfg approxConfig) (*AggregationResponse, error) {
	if len(req.GroupBy) == 0 {
		return nil, fmt.Errorf("groupBy is empty")
	}

	rows, truncated, err := e.recursiveGroupBy(idx, baseQuery, req.GroupBy, req.Aggregations, req.SubAggregations, req.Accuracy, req, cfg)
	if err != nil {
		return nil, err
	}

	resp := &AggregationResponse{Data: rows}
	if truncated {
		resp.Accuracy = "APPROXIMATE"
	}
	return resp, nil
}

// ExpandGroupByCombinations returns the list of groupBy-index subsets to run
// when cube / rollup is requested. Each subset is an ascending slice of indexes
// into the declared groupBy slice. Ordering:
//   - cube: every 2^N subset, the full set first, then decreasing mask order
//     down to the empty set (grand total last).
//   - rollup: the hierarchical chain [0..N-1], [0..N-2], ..., [0..0], []
//     (N+1 entries).
//
// Cube takes precedence when both flags are set. Exported so downstream
// aggregation paths (e.g. objectset derived-field) can reuse the same
// expansion semantics.
func ExpandGroupByCombinations(n int, cube, rollup bool) [][]int {
	if n == 0 {
		return [][]int{nil}
	}
	switch {
	case cube:
		total := 1 << n
		combos := make([][]int, 0, total)
		for mask := total - 1; mask >= 0; mask-- {
			var subset []int
			for i := 0; i < n; i++ {
				if mask&(1<<i) != 0 {
					subset = append(subset, i)
				}
			}
			combos = append(combos, subset)
		}
		return combos
	case rollup:
		combos := make([][]int, 0, n+1)
		for k := n; k > 0; k-- {
			subset := make([]int, k)
			for i := 0; i < k; i++ {
				subset[i] = i
			}
			combos = append(combos, subset)
		}
		combos = append(combos, nil)
		return combos
	default:
		subset := make([]int, n)
		for i := range subset {
			subset[i] = i
		}
		return [][]int{subset}
	}
}

// aggregateCubeOrRollup dispatches the full set of groupBy subsets implied by
// Cube/Rollup. Each subset runs through the same recursive grouping path as a
// plain request; rows are concatenated into a single flat response with
// non-grouped dimensions absent from the Group map.
func (e *Engine) aggregateCubeOrRollup(idx bleve.Index, baseQuery query.Query, req *AggregationRequest, cfg approxConfig) (*AggregationResponse, error) {
	combos := ExpandGroupByCombinations(len(req.GroupBy), req.Cube, req.Rollup)
	var allRows []AggregationRow
	var truncated bool
	for _, subset := range combos {
		if len(subset) == 0 {
			metrics, tr, approx, err := e.computeMetrics(idx, baseQuery, req.Aggregations, req.Accuracy, cfg)
			if err != nil {
				return nil, fmt.Errorf("cube/rollup grand total: %w", err)
			}
			if tr || approx {
				truncated = true
			}
			row := AggregationRow{Metrics: metrics}
			if len(req.SubAggregations) > 0 {
				subs, subTrunc, err := e.runSubAggregations(idx, baseQuery, req.SubAggregations, req.Accuracy, req)
				if err != nil {
					return nil, fmt.Errorf("cube/rollup grand total sub-aggregations: %w", err)
				}
				if subTrunc {
					truncated = true
				}
				row.SubAggregations = subs
			}
			allRows = append(allRows, row)
			continue
		}
		subsetGBs := make([]GroupBySpec, len(subset))
		for i, idx := range subset {
			subsetGBs[i] = req.GroupBy[idx]
		}
		rows, tr, err := e.recursiveGroupBy(idx, baseQuery, subsetGBs, req.Aggregations, req.SubAggregations, req.Accuracy, req, cfg)
		if err != nil {
			return nil, fmt.Errorf("cube/rollup subset %v: %w", subset, err)
		}
		if tr {
			truncated = true
		}
		allRows = append(allRows, rows...)
	}

	resp := &AggregationResponse{Data: allRows}
	if truncated {
		resp.Accuracy = "APPROXIMATE"
	}
	return resp, nil
}

// recursiveGroupBy processes groupBy specs one at a time, nesting results.
// Sub-aggregations attach to leaf rows only; intermediate groupBy levels
// pass them through unchanged. accuracyMode is forwarded to each leaf
// computeMetrics call so the per-bucket aggregations honour the request-
// level REQUIRE_ACCURATE / ALLOW_APPROXIMATE toggle. parentReq + cfg carry
// the US-465 approx-algorithm defaults so per-bucket leaves inherit the
// request-wide HLL precision / t-digest compression.
func (e *Engine) recursiveGroupBy(idx bleve.Index, baseQuery query.Query, groupBys []GroupBySpec, specs []AggregationSpec, subs []SubAggregationSpec, accuracyMode string, parentReq *AggregationRequest, cfg approxConfig) ([]AggregationRow, bool, error) {
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
			subRows, subTrunc, err := e.recursiveGroupBy(idx, entry.scopeQuery, remaining, specs, subs, accuracyMode, parentReq, cfg)
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
			metrics, leafTrunc, leafApprox, err := e.computeMetrics(idx, entry.scopeQuery, specs, accuracyMode, cfg)
			if err != nil {
				return nil, false, fmt.Errorf("compute metrics for group %v: %w", entry.value, err)
			}
			if leafTrunc || leafApprox {
				truncated = true
			}
			row := AggregationRow{
				Group:   map[string]interface{}{gb.Field: entry.value},
				Metrics: metrics,
			}
			if len(subs) > 0 {
				subResults, subTrunc, err := e.runSubAggregations(idx, entry.scopeQuery, subs, accuracyMode, parentReq)
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

		metrics, _, _, err := e.computeMetrics(idx, scopedQuery, specs, AccuracyAllowApproximate, resolveApproxConfig(nil))
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

		metrics, _, _, err := e.computeMetrics(idx, scopedQuery, specs, AccuracyAllowApproximate, resolveApproxConfig(nil))
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

		metrics, _, _, err := e.computeMetrics(idx, scopedQuery, specs, AccuracyAllowApproximate, resolveApproxConfig(nil))
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

		metrics, _, _, err := e.computeMetrics(idx, scopedQuery, specs, AccuracyAllowApproximate, resolveApproxConfig(nil))
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

		metrics, _, _, err := e.computeMetrics(idx, scopedQuery, specs, AccuracyAllowApproximate, resolveApproxConfig(nil))
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
