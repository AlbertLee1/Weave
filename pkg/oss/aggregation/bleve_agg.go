package aggregation

import (
	"math"
	"sort"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// computeMetrics computes aggregation metrics from search results.
// It scans all matching documents to compute min, max, sum, avg, count,
// standardDeviation, variance, and approximatePercentile.
func computeMetrics(idx bleve.Index, baseQuery query.Query, specs []AggregationSpec) ([]MetricValue, error) {
	metrics := make([]MetricValue, 0, len(specs))

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
				return nil, err
			}
			metrics = append(metrics, MetricValue{Name: name, Value: result.Total})

		case "min", "max", "sum", "avg":
			val, err := computeNumericAgg(idx, baseQuery, spec.Field, spec.Type)
			if err != nil {
				return nil, err
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "approximateDistinct":
			val, err := computeDistinct(idx, baseQuery, spec.Field)
			if err != nil {
				return nil, err
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "standardDeviation":
			val, err := computeStdDevOrVariance(idx, baseQuery, spec.Field, true)
			if err != nil {
				return nil, err
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "variance":
			val, err := computeStdDevOrVariance(idx, baseQuery, spec.Field, false)
			if err != nil {
				return nil, err
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})

		case "approximatePercentile":
			percentile := 50.0
			if spec.Percentile != nil {
				percentile = *spec.Percentile
			}
			val, err := computePercentile(idx, baseQuery, spec.Field, percentile)
			if err != nil {
				return nil, err
			}
			metrics = append(metrics, MetricValue{Name: name, Value: val})
		}
	}

	return metrics, nil
}

// computeNumericAgg iterates all matching documents and computes a numeric aggregate.
func computeNumericAgg(idx bleve.Index, query query.Query, field string, aggType string) (interface{}, error) {
	// Search for all documents, requesting the field value.
	searchReq := bleve.NewSearchRequest(query)
	searchReq.Size = 10000 // reasonable limit
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, err
	}

	if len(result.Hits) == 0 {
		return nil, nil
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
		return nil, nil
	}

	switch aggType {
	case "min":
		return minVal, nil
	case "max":
		return maxVal, nil
	case "sum":
		return sum, nil
	case "avg":
		return sum / float64(count), nil
	}
	return nil, nil
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

// computeStdDevOrVariance computes the standard deviation or variance of a numeric field.
// If isSqrt is true, returns the standard deviation; otherwise returns the variance.
func computeStdDevOrVariance(idx bleve.Index, q query.Query, field string, isSqrt bool) (interface{}, error) {
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = 10000
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, err
	}

	if len(result.Hits) == 0 {
		return nil, nil
	}

	// Collect all numeric values and compute mean.
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
		return nil, nil
	}

	// Compute mean.
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	// Compute variance (population variance).
	sumSqDiff := 0.0
	for _, v := range values {
		diff := v - mean
		sumSqDiff += diff * diff
	}
	variance := sumSqDiff / float64(len(values))

	if isSqrt {
		return math.Sqrt(variance), nil
	}
	return variance, nil
}

// computePercentile computes the approximate percentile of a numeric field.
// percentile is in the range [0, 100].
func computePercentile(idx bleve.Index, q query.Query, field string, percentile float64) (interface{}, error) {
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = 10000
	searchReq.Fields = []string{field}

	result, err := idx.Search(searchReq)
	if err != nil {
		return nil, err
	}

	if len(result.Hits) == 0 {
		return nil, nil
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
		return nil, nil
	}

	sort.Float64s(values)

	// Compute index using nearest-rank method.
	rank := percentile / 100.0 * float64(len(values))
	idx2 := int(math.Ceil(rank)) - 1
	if idx2 < 0 {
		idx2 = 0
	}
	if idx2 >= len(values) {
		idx2 = len(values) - 1
	}

	return values[idx2], nil
}
