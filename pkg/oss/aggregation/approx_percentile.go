package aggregation

import (
	"math"

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

	// Shift so the smallest recorded value is at least 1. HdrHistogram
	// cannot record zero or negative numbers with LowestDiscernibleValue=1.
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
			return 0, err
		}
	}

	raw := h.ValueAtPercentile(percentile)
	return float64(raw)/percentileScale - shift, nil
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
