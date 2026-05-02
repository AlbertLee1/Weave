package aggregation

import (
	"math"
	"math/rand"
	"sort"
	"testing"
)

// exactPercentileSort returns a true percentile (0–100) of the supplied
// float64 slice using nearest-rank on a sorted copy. It is the ground truth
// the t-digest accuracy gates compare against.
func exactPercentileSort(values []float64, percentile float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)
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

// gaussian10k builds a deterministic 10k-point Gaussian dataset used by
// supporting unit tests and benchmarks. mean/stddev were chosen so values
// stay strictly positive and cover multiple orders of magnitude.
func gaussian10k(seed int64) []float64 {
	const (
		n      = 10000
		mean   = 5000.0
		stddev = 1000.0
	)
	r := rand.New(rand.NewSource(seed))
	out := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		v := r.NormFloat64()*stddev + mean
		if v < 0 {
			v = 0
		}
		out = append(out, v)
	}
	return out
}

// BenchmarkExactPercentileSort provides the reference cost for the
// sort-based exact percentile path used in the accuracy assertions.
func BenchmarkExactPercentileSort(b *testing.B) {
	values := gaussian10k(42)
	for i := 0; i < b.N; i++ {
		_ = exactPercentileSort(values, 95)
	}
}
