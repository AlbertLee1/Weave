package aggregation

import (
	"fmt"
	"math"
	"sort"

	hdrhistogram "github.com/HdrHistogram/hdrhistogram-go"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// percentileScale is the fixed-point multiplier applied to float64 values
// before they are recorded into an HdrHistogram. HdrHistogram tracks int64
// values only, so we scale by 1e3 to preserve three decimal places — enough
// headroom for prices, latencies, and other numeric Foundry fields while
// keeping the trackable range well below int64 saturation.
const percentileScale = 1000.0

// computeApproxPercentileHdr returns a single percentile (0–100) over the
// supplied slice of float64 values, computed via HdrHistogram. It shifts the
// data so every recorded point is strictly positive, scales by
// percentileScale to retain decimal precision, then reverses both
// transforms on the retrieved quantile.
//
// Returns NaN when the input slice is empty; callers upstream surface that
// as a JSON null to match the existing sort-based behaviour.
//
// US-017 will extend this to request multiple percentiles in a single pass
// via Histogram.ValueAtPercentiles. US-018 locks in the ≤5% error assertion
// with a dedicated benchmark.
func computeApproxPercentileHdr(values []float64, percentile float64) (float64, error) {
	if len(values) == 0 {
		return math.NaN(), nil
	}
	h, shift, err := buildHistogram(values)
	if err != nil {
		return 0, err
	}
	raw := h.ValueAtPercentile(percentile)
	return float64(raw)/percentileScale - shift, nil
}

// buildHistogram constructs an HdrHistogram populated with the given values.
// It encapsulates the shift/scale dance so both the single-percentile and
// multi-percentile call paths can share one pass over the input data.
func buildHistogram(values []float64) (*hdrhistogram.Histogram, float64, error) {
	minV := values[0]
	maxV := values[0]
	for _, v := range values[1:] {
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}

	shift := 0.0
	if minV <= 0 {
		shift = -minV + 1
	}

	highest := int64((maxV+shift)*percentileScale) + 2
	if highest < 2 {
		highest = 2
	}

	h := hdrhistogram.New(1, highest, 3)
	for _, v := range values {
		scaled := int64((v + shift) * percentileScale)
		if scaled < 1 {
			scaled = 1
		}
		if scaled > highest {
			scaled = highest
		}
		if err := h.RecordValue(scaled); err != nil {
			return nil, 0, err
		}
	}
	return h, shift, nil
}

// computeApproxPercentilesHdr returns a set of percentiles (each 0–100)
// over the supplied slice of float64 values, computed in a SINGLE pass
// over one shared HdrHistogram. Keys in the returned map use the
// canonical `%g` formatting of each requested percentile ("50", "95",
// "99", "99.9"), so callers can round-trip them through JSON without
// a dedicated parser.
//
// Returns an empty map when the input slice is empty.
func computeApproxPercentilesHdr(values []float64, percentiles []float64) (map[string]float64, error) {
	out := make(map[string]float64, len(percentiles))
	if len(values) == 0 || len(percentiles) == 0 {
		return out, nil
	}

	h, shift, err := buildHistogram(values)
	if err != nil {
		return nil, err
	}

	for _, p := range percentiles {
		raw := h.ValueAtPercentile(p)
		out[fmt.Sprintf("%g", p)] = float64(raw)/percentileScale - shift
	}
	return out, nil
}

// approxPercentilesFromIndex is the Bleve-backed multi-percentile counterpart
// to approxPercentileFromIndex. It scans the index once, pipes the numeric
// field into buildHistogram exactly once, and returns a map[string]float64
// keyed by percentile string. The triple shape mirrors the legacy scalar
// path so bleve_agg.go can dispatch uniformly.
func approxPercentilesFromIndex(idx bleve.Index, q query.Query, field string, percentiles []float64, scanSize int) (interface{}, bool, error) {
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

	if len(values) == 0 {
		return nil, truncated, nil
	}

	out, err := computeApproxPercentilesHdr(values, percentiles)
	if err != nil {
		return nil, false, err
	}
	return out, truncated, nil
}

// exactPercentileFromIndex computes percentile(s) exactly via a sorted scan
// of the matching docs. Used by computeMetrics when the request carries
// AccuracyRequireAccurate so callers can opt out of the HdrHistogram path
// without rewriting their specs. The shape mirrors approxPercentilesFromIndex
// (single-percentile → float64, multi-percentile → map[string]float64) so
// the response stays JSON-shape-compatible with the approximate output.
func exactPercentileFromIndex(idx bleve.Index, q query.Query, field string, single *float64, multi []float64, scanSize int) (interface{}, bool, error) {
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = scanSize
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, false, err
	}
	truncated := result.Total > uint64(len(result.Hits))

	if len(result.Hits) == 0 {
		if len(multi) > 0 {
			return map[string]float64{}, truncated, nil
		}
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
	if len(values) == 0 {
		if len(multi) > 0 {
			return map[string]float64{}, truncated, nil
		}
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

// approxPercentileFromIndex replaces the legacy sort-based computePercentile
// with an HdrHistogram-backed path. It preserves the same (value, truncated,
// error) triple so bleve_agg.go can swap implementations without touching
// its switch arms or the truncation bookkeeping.
func approxPercentileFromIndex(idx bleve.Index, q query.Query, field string, percentile float64, scanSize int) (interface{}, bool, error) {
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

	if len(values) == 0 {
		return nil, truncated, nil
	}

	p, err := computeApproxPercentileHdr(values, percentile)
	if err != nil {
		return nil, false, err
	}
	return p, truncated, nil
}
