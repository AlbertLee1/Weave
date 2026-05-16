package bench

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"testing"

	"github.com/axiomhq/hyperloglog"
	"github.com/influxdata/tdigest"

	"github.com/liyang/weave/pkg/oss/aggregation"
)

// US-465 — HLL / t-digest accuracy gates + benchmarks at the PRD-declared
// 1M cardinality. These live in bench/ alongside US-441/US-464 latency
// contracts so the cross-subsystem regression suite captures the approx-
// aggregation accuracy budget too. They are NOT part of the eight US-441
// baseline.json benchmarks — coverage_test.go does not gate them, and
// the budget here is the PRD's "<2% HLL p99, <1% t-digest p99" wording,
// not a 20% nsPerOp delta.
//
// Tests:
//   - TestApproximateDistinct_OneMillion_HLL_US465_P99WithinBudget — HLL
//     default precision (14), 1M unique inserts × 25 trials, asserts p99
//     of relative error stays under 2%.
//   - TestApproximatePercentile_OneMillion_TDigest_US465_P99WithinBudget —
//     t-digest default compression (100), 1M uniform-random points across
//     9 percentile probes (p1..p99.9), asserts p99 of relative error stays
//     under 1%.
//
// Benchmarks (one each, exported under the same name root so `go test
// -bench=US465` runs both):
//   - BenchmarkApproximateDistinct_OneMillion_US465_HLL — measures Insert
//     + Estimate over 1M items at default precision.
//   - BenchmarkApproximatePercentile_OneMillion_US465_TDigest — measures
//     Add + Quantile over 1M points at default compression.

// us465HLLTrials / us465PercentileProbes pin the sampling parameters once
// so the test and the bench both ramp from the same cost contract.
const us465HLLTrials = 25

var us465PercentileProbes = []float64{1, 10, 50, 75, 90, 95, 99, 99.5, 99.9}

// TestApproximateDistinct_OneMillion_HLL_US465_P99WithinBudget locks the
// PRD HLL accuracy gate: across 25 seeds, the p99 of |est-1M|/1M stays
// below 2% at default precision 14. Twenty-five trials is the same sample
// budget used by the searchAround latency gate in bench/searcharound_us464.
func TestApproximateDistinct_OneMillion_HLL_US465_P99WithinBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1M HLL accuracy gate in -short mode")
	}
	errs := make([]float64, 0, us465HLLTrials)
	for seed := int64(1); seed <= int64(us465HLLTrials); seed++ {
		errs = append(errs, hllRelativeErrorAtOneMillion(t, seed, aggregation.DefaultHLLPrecision))
	}
	sort.Float64s(errs)
	p99 := errs[int(math.Ceil(0.99*float64(us465HLLTrials)))-1]
	if p99 > 0.02 {
		t.Fatalf("1M HLL p99 relative error %.6f > 0.02 over %d trials (max=%.6f)", p99, us465HLLTrials, errs[us465HLLTrials-1])
	}
	t.Logf("1M HLL p99 = %.6f (max=%.6f, min=%.6f, precision=%d)", p99, errs[us465HLLTrials-1], errs[0], aggregation.DefaultHLLPrecision)
}

// TestApproximatePercentile_OneMillion_TDigest_US465_P99WithinBudget locks
// the PRD t-digest accuracy gate at default compression (100): across nine
// percentile probes spanning the tails to the median, the p99 of relative
// error stays below 1%.
func TestApproximatePercentile_OneMillion_TDigest_US465_P99WithinBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1M t-digest accuracy gate in -short mode")
	}
	const n = 1_000_000
	r := rand.New(rand.NewSource(465))
	values := make([]float64, n)
	for i := range values {
		values[i] = r.Float64()
	}
	exactSorted := append([]float64(nil), values...)
	sort.Float64s(exactSorted)

	errs := make([]float64, 0, len(us465PercentileProbes))
	for _, p := range us465PercentileProbes {
		exact := nearestRank(exactSorted, p)
		if exact == 0 {
			continue
		}
		approx := tdigestPercentile(values, p, aggregation.DefaultTDigestCompression)
		rel := math.Abs(approx-exact) / math.Abs(exact)
		errs = append(errs, rel)
		t.Logf("p%v exact=%.6f approx=%.6f rel=%.6f", p, exact, approx, rel)
	}
	sort.Float64s(errs)
	p99 := errs[int(math.Ceil(0.99*float64(len(errs))))-1]
	if p99 > 0.01 {
		t.Fatalf("1M t-digest p99 relative error %.6f > 0.01 across probes %v", p99, us465PercentileProbes)
	}
}

// BenchmarkApproximateDistinct_OneMillion_US465_HLL measures Insert+Estimate
// at default precision (14) for a 1M-item stream. The bench is not gated on
// nsPerOp; the matching accuracy gate is the *test* above.
func BenchmarkApproximateDistinct_OneMillion_US465_HLL(b *testing.B) {
	const cardinality = 1_000_000
	keys := make([][]byte, cardinality)
	r := rand.New(rand.NewSource(465))
	for i := 0; i < cardinality; i++ {
		keys[i] = []byte(fmt.Sprintf("k-%d-%d", i, r.Int63()))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sketch, err := hyperloglog.NewSketch(aggregation.DefaultHLLPrecision, true)
		if err != nil {
			b.Fatalf("new sketch: %v", err)
		}
		for _, k := range keys {
			sketch.Insert(k)
		}
		_ = sketch.Estimate()
	}
}

// BenchmarkApproximatePercentile_OneMillion_US465_TDigest measures Add +
// Quantile at default compression (100) for a 1M-point uniform stream.
func BenchmarkApproximatePercentile_OneMillion_US465_TDigest(b *testing.B) {
	const n = 1_000_000
	r := rand.New(rand.NewSource(465))
	values := make([]float64, n)
	for i := range values {
		values[i] = r.Float64()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tdigestPercentile(values, 99, aggregation.DefaultTDigestCompression)
	}
}

// hllRelativeErrorAtOneMillion seeds a fresh HLL sketch with `cardinality`
// unique inserts and returns the relative error of its Estimate vs the true
// cardinality. Used by the test gate to sample the error distribution.
func hllRelativeErrorAtOneMillion(t *testing.T, seed int64, precision uint8) float64 {
	t.Helper()
	const cardinality = 1_000_000
	sketch, err := hyperloglog.NewSketch(precision, true)
	if err != nil {
		t.Fatalf("new sketch (precision=%d): %v", precision, err)
	}
	r := rand.New(rand.NewSource(seed))
	for i := 0; i < cardinality; i++ {
		sketch.Insert([]byte(fmt.Sprintf("%d-%d-%d", seed, i, r.Int63())))
	}
	est := float64(sketch.Estimate())
	return math.Abs(est-float64(cardinality)) / float64(cardinality)
}

// tdigestPercentile is the bench-local single-percentile helper. Kept here
// rather than calling the unexported pkg-internal helper so the bench file
// stays self-contained and can target any compression value end-to-end.
func tdigestPercentile(values []float64, percentile, compression float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	td := tdigest.NewWithCompression(compression)
	for _, v := range values {
		td.Add(v, 1)
	}
	return td.Quantile(percentile / 100.0)
}

// nearestRank returns the exact percentile of a pre-sorted slice. Used by
// the bench-local accuracy gate so it has a ground truth independent of the
// aggregation package's internal helper of the same name.
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
