package aggregation

import (
	"fmt"
	"math"

	"github.com/axiomhq/hyperloglog"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// DefaultHLLPrecision is the HyperLogLog precision used for approximateDistinct
// when a caller does not specify one. At p=14 the standard error is
// 1.04/sqrt(2^14) ≈ 0.81%, comfortably under the PRD's <=1% ceiling while
// keeping the sketch at 16 KiB per aggregator.
const DefaultHLLPrecision = 14

// MinHLLPrecision / MaxHLLPrecision bound the HyperLogLog precision parameter
// exposed to callers. axiomhq/hyperloglog accepts p in [4, 18]; we surface
// the same range and reject out-of-range values at validation time so the
// caller gets a clean 400 rather than a panic from deep inside the sketch.
const (
	MinHLLPrecision = 4
	MaxHLLPrecision = 18
)

// computeMetrics computes aggregation metrics from search results.
// It scans matching documents to compute min, max, sum, avg, count,
// standardDeviation, variance, and approximatePercentile. The second
// return value is true when any numeric scan was truncated because the
// match total exceeded the engine's MaxDocScanSize — the caller uses it
// to mark the top-level response as APPROXIMATE. The third return value
// is true when at least one approximate algorithm (HLL distinct,
// t-digest percentile) actually produced an approximate result —
// also surfaced as APPROXIMATE on the response. accuracyMode is the
// Palantir request-level toggle: AccuracyRequireAccurate transparently
// promotes approximateDistinct → exactDistinct and approximatePercentile
// → sort-based exact percentile so callers that need byte-exact output
// can opt out of the sketches without rewriting their specs.
func (e *Engine) computeMetrics(idx bleve.Index, baseQuery query.Query, specs []AggregationSpec, accuracyMode string) ([]MetricValue, bool, bool, error) {
	metrics := make([]MetricValue, 0, len(specs))
	truncated := false
	approximate := false
	exact := requireAccurate(accuracyMode)

	scanSize := e.MaxDocScanSize
	if scanSize <= 0 {
		scanSize = 10000
	}

	for _, spec := range specs {
		name := spec.Name
		if name == "" {
			name = spec.Type
			if spec.Field != "" {
				name = spec.Field + "." + spec.Type
			}
		}

		switch spec.Type {
		case "count":
			searchReq := bleve.NewSearchRequest(baseQuery)
			searchReq.Size = 0
			result, err := idx.Search(searchReq)
			if err != nil {
				return nil, false, false, err
			}
			metrics = append(metrics, MetricValue{Name: name, Value: result.Total})

		case "min", "max", "sum", "avg":
			val, t, err := computeNumericAgg(idx, baseQuery, spec.Field, spec.Type, scanSize)
			if err != nil {
				return nil, false, false, err
			}
			if t {
				truncated = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "approximateDistinct":
			if exact {
				val, t, err := computeExactDistinct(idx, baseQuery, spec.Field, scanSize)
				if err != nil {
					return nil, false, false, err
				}
				if t {
					truncated = true
				}
				metrics = append(metrics, MetricValue{Name: name, Value: val})
				break
			}
			precision := DefaultHLLPrecision
			if spec.Precision != nil {
				precision = *spec.Precision
			}
			if precision < MinHLLPrecision || precision > MaxHLLPrecision {
				return nil, false, false, fmt.Errorf("approximateDistinct: precision %d out of range [%d,%d]", precision, MinHLLPrecision, MaxHLLPrecision)
			}
			val, t, approx, err := computeApproximateDistinct(idx, baseQuery, spec.Field, uint8(precision), scanSize)
			if err != nil {
				return nil, false, false, err
			}
			if t {
				truncated = true
			}
			if approx {
				approximate = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "exactDistinct":
			val, t, err := computeExactDistinct(idx, baseQuery, spec.Field, scanSize)
			if err != nil {
				return nil, false, false, err
			}
			if t {
				truncated = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "standardDeviation":
			val, t, err := computeStdDevOrVariance(idx, baseQuery, spec.Field, true, scanSize)
			if err != nil {
				return nil, false, false, err
			}
			if t {
				truncated = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "variance":
			val, t, err := computeStdDevOrVariance(idx, baseQuery, spec.Field, false, scanSize)
			if err != nil {
				return nil, false, false, err
			}
			if t {
				truncated = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "collectList":
			maxItems := 100
			if spec.MaxItems != nil {
				maxItems = *spec.MaxItems
			}
			val, t, err := computeCollectList(idx, baseQuery, spec.Field, scanSize, maxItems)
			if err != nil {
				return nil, false, false, err
			}
			if t {
				truncated = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "approximatePercentile":
			if exact {
				val, t, err := exactPercentileFromIndex(idx, baseQuery, spec.Field, spec.Percentile, spec.Percentiles, scanSize)
				if err != nil {
					return nil, false, false, err
				}
				if t {
					truncated = true
				}
				metrics = append(metrics, MetricValue{Name: name, Value: val})
				break
			}
			if len(spec.Percentiles) > 0 {
				val, t, err := approxPercentilesFromIndex(idx, baseQuery, spec.Field, spec.Percentiles, scanSize)
				if err != nil {
					return nil, false, false, err
				}
				if t {
					truncated = true
				}
				if val != nil {
					approximate = true
				}
				metrics = append(metrics, MetricValue{Name: name, Value: val})
			} else {
				percentile := 50.0
				if spec.Percentile != nil {
					percentile = *spec.Percentile
				}
				val, t, err := approxPercentileFromIndex(idx, baseQuery, spec.Field, percentile, scanSize)
				if err != nil {
					return nil, false, false, err
				}
				if t {
					truncated = true
				}
				if val != nil {
					approximate = true
				}
				metrics = append(metrics, MetricValue{Name: name, Value: val})
			}
		}
	}

	return metrics, truncated, approximate, nil
}

// computeNumericAgg iterates matching documents and computes a numeric aggregate.
// It returns (value, truncated, error) where truncated is true when the match
// total exceeds scanSize.
func computeNumericAgg(idx bleve.Index, query query.Query, field string, aggType string, scanSize int) (interface{}, bool, error) {
	searchReq := bleve.NewSearchRequest(query)
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

	var minVal, maxVal, sum float64
	count := 0
	minVal = math.MaxFloat64
	maxVal = -math.MaxFloat64

	for _, hit := range result.Hits {
		val, ok := hit.Fields[field]
		if !ok {
			continue
		}
		numVal, ok := val.(float64)
		if !ok {
			continue
		}
		count++
		sum += numVal
		if numVal < minVal {
			minVal = numVal
		}
		if numVal > maxVal {
			maxVal = numVal
		}
	}

	if count == 0 {
		return nil, truncated, nil
	}

	switch aggType {
	case "min":
		return minVal, truncated, nil
	case "max":
		return maxVal, truncated, nil
	case "sum":
		return sum, truncated, nil
	case "avg":
		return sum / float64(count), truncated, nil
	}
	return nil, truncated, nil
}

// computeApproximateDistinct estimates the cardinality of a field using a
// HyperLogLog sketch seeded per-call. Values are scanned from the documents
// matching the query (up to scanSize), fed to the sketch as their byte
// representation, and the sketch's Estimate() is returned as an int.
//
// Returns (estimate, scanTruncated, approximate, err).
//   - scanTruncated is true when the match total exceeds scanSize — the caller
//     surfaces that as APPROXIMATE accuracy because we did not see every doc.
//   - approximate is true when the sketch's final estimate crosses the sparse
//     →dense threshold and the returned value is therefore an estimate rather
//     than an exact count. axiomhq's sparse compressed-list representation
//     gives EXACT counts for low cardinalities (transition is implementation-
//     specific but bounded above by ~m/4 inserts at p=14, with m=1<<precision).
//     We use the conservative threshold 1<<(precision-2) so any cardinality
//     comfortably below the sparse boundary reports approximate=false; this
//     keeps the response.accuracy=ACCURATE invariant for callers that pre-date
//     the HLL switch and relied on Bleve facets returning exact small-N counts.
func computeApproximateDistinct(idx bleve.Index, q query.Query, field string, precision uint8, scanSize int) (int, bool, bool, error) {
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = scanSize
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return 0, false, false, err
	}

	truncated := result.Total > uint64(len(result.Hits))

	sketch, err := hyperloglog.NewSketch(precision, true)
	if err != nil {
		return 0, false, false, fmt.Errorf("new hll sketch (precision=%d): %w", precision, err)
	}

	for _, hit := range result.Hits {
		val, ok := hit.Fields[field]
		if !ok {
			continue
		}
		switch v := val.(type) {
		case string:
			sketch.Insert([]byte(v))
		case []interface{}:
			for _, item := range v {
				sketch.Insert([]byte(fmt.Sprint(item)))
			}
		default:
			sketch.Insert([]byte(fmt.Sprint(v)))
		}
	}

	estimate := int(sketch.Estimate())
	approximate := estimate >= sparseExactThreshold(precision)
	return estimate, truncated, approximate, nil
}

// sparseExactThreshold returns the cardinality below which the HLL sketch is
// guaranteed to be in its sparse exact-counting representation. axiomhq's
// transition from sparse to dense happens implementation-specifically when
// the compressed sparse list grows beyond the dense storage cost; the
// conservative bound 1<<(precision-2) sits comfortably below that crossover
// for every supported precision and keeps the "small-N is exact" guarantee.
func sparseExactThreshold(precision uint8) int {
	if precision < MinHLLPrecision {
		precision = MinHLLPrecision
	}
	return 1 << (precision - 2)
}

// computeExactDistinct counts exact distinct values for a field using map-based deduplication.
// Unlike computeDistinct (which uses Bleve facets with a term limit), this scans documents
// and deduplicates with a map for an exact count.
func computeExactDistinct(idx bleve.Index, q query.Query, field string, scanSize int) (int, bool, error) {
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = scanSize
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return 0, false, err
	}

	truncated := result.Total > uint64(len(result.Hits))

	seen := make(map[string]struct{})
	for _, hit := range result.Hits {
		val, ok := hit.Fields[field]
		if !ok {
			continue
		}
		seen[fmt.Sprint(val)] = struct{}{}
	}

	return len(seen), truncated, nil
}

// computeCollectList collects field values into a list.
// Returns ([]interface{}, truncated, error) where truncated is true when the match
// total exceeds scanSize.
func computeCollectList(idx bleve.Index, q query.Query, field string, scanSize int, maxItems int) ([]interface{}, bool, error) {
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = scanSize
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, false, err
	}

	truncated := result.Total > uint64(len(result.Hits))

	var values []interface{}
	for _, hit := range result.Hits {
		if len(values) >= maxItems {
			break
		}
		val, ok := hit.Fields[field]
		if !ok {
			continue
		}
		values = append(values, val)
	}

	if values == nil {
		values = []interface{}{}
	}

	return values, truncated, nil
}

// computeStdDevOrVariance computes the standard deviation or variance of a numeric field.
// If isSqrt is true, returns the standard deviation; otherwise returns the variance.
// Returns (value, truncated, error).
func computeStdDevOrVariance(idx bleve.Index, q query.Query, field string, isSqrt bool, scanSize int) (interface{}, bool, error) {
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

	var values []float64
	for _, hit := range result.Hits {
		val, ok := hit.Fields[field]
		if !ok {
			continue
		}
		numVal, ok := val.(float64)
		if !ok {
			continue
		}
		values = append(values, numVal)
	}

	if len(values) == 0 {
		return nil, truncated, nil
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	sumSqDiff := 0.0
	for _, v := range values {
		diff := v - mean
		sumSqDiff += diff * diff
	}
	variance := sumSqDiff / float64(len(values))

	if isSqrt {
		return math.Sqrt(variance), truncated, nil
	}
	return variance, truncated, nil
}
