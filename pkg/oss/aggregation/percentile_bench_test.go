package aggregation

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"
)

// exactPercentileSort returns a true percentile (0–100) of the supplied
// float64 slice using nearest-rank on a sorted copy. It is the ground truth
// that US-018 compares the HdrHistogram approximate path against.
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
// both the accuracy bench and supporting unit test. mean/stddev were chosen
// so values stay strictly positive and cover multiple orders of magnitude.
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

// TestApproxPercentileAccuracy_5PercentBound is the US-018 accuracy gate.
// It runs during `go test ./...` (independent of -bench) so accuracy
// regressions fail CI even when benchmarks are skipped. The 5% error bound
// matches the Foundry parity requirement captured in tasks/prd-v2-deep-parity.md.
func TestApproxPercentileAccuracy_5PercentBound(t *testing.T) {
	values := gaussian10k(42)
	for _, p := range []float64{50, 95, 99} {
		exact := exactPercentileSort(values, p)
		approx, err := computeApproxPercentileHdr(values, p)
		if err != nil {
			t.Fatalf("computeApproxPercentileHdr p%v: %v", p, err)
		}
		if exact == 0 {
			t.Fatalf("exact p%v = 0, refuse to divide by zero", p)
		}
		rel := math.Abs(approx-exact) / math.Abs(exact)
		if rel > 0.05 {
			t.Fatalf("p%v relative error %.4f > 0.05 (exact=%.4f approx=%.4f)",
				p, rel, exact, approx)
		}
		t.Logf("p%v exact=%.4f approx=%.4f rel=%.6f", p, exact, approx, rel)
	}
}

// BenchmarkApproxPercentile times the HdrHistogram-backed percentile over
// 10k points and simultaneously asserts |approx - exact| / exact <= 0.05.
// The accuracy check lives inside b.Fatalf so a regression is treated as
// a benchmark failure rather than a silent slowdown — satisfying US-018
// "Bench failure fails the test (not just slow)".
func BenchmarkApproxPercentile(b *testing.B) {
	values := gaussian10k(42)
	percentiles := []float64{50, 95, 99}

	exact := make(map[float64]float64, len(percentiles))
	for _, p := range percentiles {
		exact[p] = exactPercentileSort(values, p)
	}

	for _, p := range percentiles {
		pv := p
		b.Run(fmt.Sprintf("p%g", pv), func(b *testing.B) {
			var approx float64
			for i := 0; i < b.N; i++ {
				got, err := computeApproxPercentileHdr(values, pv)
				if err != nil {
					b.Fatalf("computeApproxPercentileHdr: %v", err)
				}
				approx = got
			}
			rel := math.Abs(approx-exact[pv]) / math.Abs(exact[pv])
			if rel > 0.05 {
				b.Fatalf("p%g relative error %.4f > 0.05 (exact=%.4f approx=%.4f)",
					pv, rel, exact[pv], approx)
			}
		})
	}
}

// BenchmarkApproxPercentileMulti times the single-pass multi-percentile
// path that powers US-017. It asserts each requested percentile stays
// within the 5% error bound.
func BenchmarkApproxPercentileMulti(b *testing.B) {
	values := gaussian10k(42)
	percentiles := []float64{50, 95, 99}

	exact := make(map[string]float64, len(percentiles))
	for _, p := range percentiles {
		exact[fmt.Sprintf("%g", p)] = exactPercentileSort(values, p)
	}

	var got map[string]float64
	for i := 0; i < b.N; i++ {
		out, err := computeApproxPercentilesHdr(values, percentiles)
		if err != nil {
			b.Fatalf("computeApproxPercentilesHdr: %v", err)
		}
		got = out
	}

	for key, want := range exact {
		have, ok := got[key]
		if !ok {
			b.Fatalf("missing percentile %q in result", key)
		}
		rel := math.Abs(have-want) / math.Abs(want)
		if rel > 0.05 {
			b.Fatalf("p%s relative error %.4f > 0.05 (exact=%.4f approx=%.4f)",
				key, rel, want, have)
		}
	}
}

// BenchmarkExactPercentileSort provides the reference cost for the
// sort-based exact percentile path used in the accuracy assertions.
// Recorded in bench/aggregation_percentile.md alongside the approx numbers.
func BenchmarkExactPercentileSort(b *testing.B) {
	values := gaussian10k(42)
	for i := 0; i < b.N; i++ {
		_ = exactPercentileSort(values, 95)
	}
}
