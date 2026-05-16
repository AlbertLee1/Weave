package aggregation

import (
	"fmt"
	"math"
	"sort"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// approxPercentilesFromIndex is the Bleve-backed multi-percentile counterpart
// to approxPercentileFromIndex. It scans the index once, feeds every numeric
// hit into a single bounded t-digest at the supplied compression, and returns
// a map[string]float64 keyed by percentile string. The triple shape mirrors
// the legacy scalar path so bleve_agg.go can dispatch uniformly. compression
// is the request- or spec-level US-465 override.
func approxPercentilesFromIndex(idx bleve.Index, q query.Query, field string, percentiles []float64, scanSize int, compression float64) (interface{}, bool, error) {
	values, truncated, err := scanNumericField(idx, q, field, scanSize)
	if err != nil {
		return nil, false, err
	}
	if len(values) == 0 {
		return nil, truncated, nil
	}
	return computeApproxPercentilesTDigestC(values, percentiles, compression), truncated, nil
}

// exactPercentileFromIndex computes percentile(s) exactly via a sorted scan
// of the matching docs. Used by computeMetrics when the request carries
// AccuracyRequireAccurate so callers can opt out of the t-digest path
// without rewriting their specs. The shape mirrors approxPercentilesFromIndex
// (single-percentile → float64, multi-percentile → map[string]float64) so
// the response stays JSON-shape-compatible with the approximate output.
func exactPercentileFromIndex(idx bleve.Index, q query.Query, field string, single *float64, multi []float64, scanSize int) (interface{}, bool, error) {
	values, truncated, err := scanNumericField(idx, q, field, scanSize)
	if err != nil {
		return nil, false, err
	}
	if len(values) == 0 {
		// Both single- and multi-percentile shapes collapse to nil on an
		// empty input so the response stays shape-uniform across the approx
		// and exact paths (the approx path also returns nil on empty input).
		return nil, truncated, nil
	}
	sort.Float64s(values)

	if len(multi) > 0 {
		out := make(map[string]float64, len(multi))
		for _, p := range multi {
			out[fmt.Sprintf("%g", p)] = nearestRank(values, p)
		}
		return out, truncated, nil
	}
	p := 50.0
	if single != nil {
		p = *single
	}
	return nearestRank(values, p), truncated, nil
}

// nearestRank returns the value at the requested percentile (0–100) of the
// pre-sorted slice using nearest-rank, matching the existing test helper
// exactPercentileSort. Defined alongside exactPercentileFromIndex so the
// production path doesn't depend on test-only code.
func nearestRank(sorted []float64, percentile float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	if percentile <= 0 {
		return sorted[0]
	}
	if percentile >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := int(math.Ceil(percentile/100.0*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// approxPercentileFromIndex routes the single-percentile approximate path
// through the bounded t-digest at the supplied compression. It preserves
// the same (value, truncated, error) triple so bleve_agg.go can dispatch
// uniformly with the multi path and the exact-path fallback. compression
// is the request- or spec-level US-465 override.
func approxPercentileFromIndex(idx bleve.Index, q query.Query, field string, percentile float64, scanSize int, compression float64) (interface{}, bool, error) {
	values, truncated, err := scanNumericField(idx, q, field, scanSize)
	if err != nil {
		return nil, false, err
	}
	if len(values) == 0 {
		return nil, truncated, nil
	}
	return computeApproxPercentileTDigestC(values, percentile, compression), truncated, nil
}

// scanNumericField fetches up to scanSize hits from the index and pulls out
// the numeric value of the requested field on each hit. Returns the slice
// plus a truncation flag (true when the match total exceeded the scan size,
// in which case the response.accuracy gets bumped to APPROXIMATE upstream).
// Centralised so every percentile path shares one extraction loop.
func scanNumericField(idx bleve.Index, q query.Query, field string, scanSize int) ([]float64, bool, error) {
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = scanSize
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, false, err
	}
	truncated := result.Total > uint64(len(result.Hits))

	if len(result.Hits) == 0 {
		return nil, truncated, nil
	}

	values := make([]float64, 0, len(result.Hits))
	for _, hit := range result.Hits {
		raw, ok := hit.Fields[field]
		if !ok {
			continue
		}
		numVal, ok := raw.(float64)
		if !ok {
			continue
		}
		values = append(values, numVal)
	}
	return values, truncated, nil
}
