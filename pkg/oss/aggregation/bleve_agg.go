package aggregation

import (
	"fmt"
	"math"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// computeMetrics computes aggregation metrics from search results.
// It scans matching documents to compute min, max, sum, avg, count,
// standardDeviation, variance, and approximatePercentile. The second
// return value is true when any numeric scan was truncated because the
// match total exceeded the engine's MaxDocScanSize — the caller uses it
// to mark the top-level response as APPROXIMATE.
func (e *Engine) computeMetrics(idx bleve.Index, baseQuery query.Query, specs []AggregationSpec) ([]MetricValue, bool, error) {
	metrics := make([]MetricValue, 0, len(specs))
	truncated := false

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
				return nil, false, err
			}
			metrics = append(metrics, MetricValue{Name: name, Value: result.Total})

		case "min", "max", "sum", "avg":
			val, t, err := computeNumericAgg(idx, baseQuery, spec.Field, spec.Type, scanSize)
			if err != nil {
				return nil, false, err
			}
			if t {
				truncated = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "approximateDistinct":
			val, err := computeDistinct(idx, baseQuery, spec.Field)
			if err != nil {
				return nil, false, err
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "exactDistinct":
			val, t, err := computeExactDistinct(idx, baseQuery, spec.Field, scanSize)
			if err != nil {
				return nil, false, err
			}
			if t {
				truncated = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "standardDeviation":
			val, t, err := computeStdDevOrVariance(idx, baseQuery, spec.Field, true, scanSize)
			if err != nil {
				return nil, false, err
			}
			if t {
				truncated = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "variance":
			val, t, err := computeStdDevOrVariance(idx, baseQuery, spec.Field, false, scanSize)
			if err != nil {
				return nil, false, err
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
				return nil, false, err
			}
			if t {
				truncated = true
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "approximatePercentile":
			if len(spec.Percentiles) > 0 {
				val, t, err := approxPercentilesFromIndex(idx, baseQuery, spec.Field, spec.Percentiles, scanSize)
				if err != nil {
					return nil, false, err
				}
				if t {
					truncated = true
				}
				metrics = append(metrics, MetricValue{Name: name, Value: val})
			} else {
				percentile := 50.0
				if spec.Percentile != nil {
					percentile = *spec.Percentile
				}
				val, t, err := approxPercentileFromIndex(idx, baseQuery, spec.Field, percentile, scanSize)
				if err != nil {
					return nil, false, err
				}
				if t {
					truncated = true
				}
				metrics = append(metrics, MetricValue{Name: name, Value: val})
			}
		}
	}

	return metrics, truncated, nil
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

// computeDistinct counts approximate distinct values for a field.
func computeDistinct(idx bleve.Index, query query.Query, field string) (int, error) {
	searchReq := bleve.NewSearchRequest(query)
	searchReq.Size = 0
	facet := bleve.NewFacetRequest(field, 10000)
	searchReq.AddFacet(field+"_distinct", facet)

	result, err := idx.Search(searchReq)
	if err != nil {
		return 0, err
	}

	facetResult, ok := result.Facets[field+"_distinct"]
	if !ok {
		return 0, nil
	}

	if facetResult.Terms == nil {
		return 0, nil
	}
	return len(facetResult.Terms.Terms()), nil
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
