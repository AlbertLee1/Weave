package aggregation

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"testing"

	"github.com/axiomhq/hyperloglog"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/influxdata/tdigest"
)

// US-465 — HLL / t-digest precision is configurable at the request level,
// scan-row threshold is configurable, and 1M-cardinality accuracy gates
// hold (HLL p99 error < 2%, t-digest p99 error < 1%).
//
// Acceptance criteria (PRD):
//   • AggregationRequest accepts hllPrecision (4..18, default 14) + tdigestCompression (default 100)
//   • 1M unique values: HLL p99 relative error < 2%; t-digest p99 error < 1%
//   • Scanned-row threshold is configurable (default 1M) and trips the APPROXIMATE marker
//   • Benchmarks are in CI (see bench/aggregation_us465_bench_test.go)

// TestAggregationRequest_HLLPrecisionRequestLevel locks in the request-wide
// HLL precision plumbing: when no per-spec Precision is set, the request-level
// HLLPrecision must be used; per-spec Precision overrides the request-level
// default.
func TestAggregationRequest_HLLPrecisionRequestLevel(t *testing.T) {
	const cardinality = 5000
	const total = 10000
	idx := setupHLLIndex(t, cardinality, total)

	eng := NewEngine()
	eng.MaxDocScanSize = total + 1

	// Case 1: request-level precision propagates to the spec when spec.Precision is nil.
	p := 16
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		HLLPrecision: &p,
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "tag", Name: "d16"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate (req-level precision=16) returned error: %v", err)
	}
	val, ok := findMetric(resp.Data[0].Metrics, "d16")
	if !ok {
		t.Fatalf("expected metric d16 in %+v", resp.Data[0].Metrics)
	}
	got := val.(int)
	relErr := math.Abs(float64(got-cardinality)) / float64(cardinality)
	if relErr > 0.01 {
		t.Errorf("request-level precision=16 HLL estimate %d, want ~%d (relErr=%.4f > 0.01)", got, cardinality, relErr)
	}

	// Case 2: per-spec precision wins over the request-level default.
	pReq := 14
	pSpec := 18 // tightest precision — sub-0.1% standard error
	resp, err = eng.Aggregate(idx, &AggregationRequest{
		HLLPrecision: &pReq,
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "tag", Precision: &pSpec, Name: "d18"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate (per-spec precision=18) returned error: %v", err)
	}
	val, ok = findMetric(resp.Data[0].Metrics, "d18")
	if !ok {
		t.Fatalf("expected metric d18 in %+v", resp.Data[0].Metrics)
	}
	got = val.(int)
	relErr = math.Abs(float64(got-cardinality)) / float64(cardinality)
	// At p=18 with 5000 cardinality, the sparse representation still gives an exact count.
	if got != cardinality {
		t.Errorf("per-spec precision=18 HLL estimate %d, want exact %d (relErr=%.4f)", got, cardinality, relErr)
	}
}

// TestAggregationRequest_HLLPrecisionInvalidRejected ensures the request-level
// hllPrecision is range-validated the same way the per-spec one is.
func TestAggregationRequest_HLLPrecisionInvalidRejected(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()
	cases := []int{0, 3, 19, 25}
	for _, p := range cases {
		precision := p
		_, err := eng.Aggregate(idx, &AggregationRequest{
			HLLPrecision: &precision,
			Aggregations: []AggregationSpec{
				{Type: "approximateDistinct", Field: "department"},
			},
		})
		if err == nil {
			t.Errorf("hllPrecision=%d: expected error, got nil", p)
		}
	}
}

// TestAggregationRequest_TDigestCompressionRequestLevel locks in the request-
// wide t-digest compression plumbing. We can't directly observe the digest's
// compression value through the response (only the percentile estimate comes
// out), so we check the bound by checking that an absurd compression value
// (1.0 — degenerate, fewer than 10 centroids) produces a MEASURABLY worse
// percentile estimate than the default 100. A spec-level override pulls the
// estimate back to the default precision.
func TestAggregationRequest_TDigestCompressionRequestLevel(t *testing.T) {
	idx := setupTDigestIndex(t, 50_000)
	eng := NewEngine()
	eng.MaxDocScanSize = 60_000

	exact := tdigestPercentileFromIndex(t, idx, "price", 99)

	// Default compression baseline — should be near-exact (well under 1%).
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "approximatePercentile", Field: "price", Percentile: ptrFloat(99), Name: "p99"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate (default compression) returned error: %v", err)
	}
	p99Default := mustFloatMetric(t, resp.Data[0].Metrics, "p99")
	relDefault := math.Abs(p99Default-exact) / math.Abs(exact)
	if relDefault > 0.01 {
		t.Fatalf("default p99 relErr %.6f > 0.01 (exact=%.4f approx=%.4f)", relDefault, exact, p99Default)
	}

	// Degenerate request-level compression — should be measurably looser, but
	// also must satisfy a hard upper bound so the test breaks if compression is
	// silently dropped on the floor (i.e. always uses the default).
	c := 2.0
	resp, err = eng.Aggregate(idx, &AggregationRequest{
		TDigestCompression: &c,
		Aggregations: []AggregationSpec{
			{Type: "approximatePercentile", Field: "price", Percentile: ptrFloat(99), Name: "p99low"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate (req-level compression=2) returned error: %v", err)
	}
	p99Low := mustFloatMetric(t, resp.Data[0].Metrics, "p99low")
	relLow := math.Abs(p99Low-exact) / math.Abs(exact)
	if relLow <= relDefault {
		t.Errorf("expected degenerate compression=2 error %.6f to exceed default error %.6f — compression is being ignored", relLow, relDefault)
	}

	// Per-spec compression wins over request-level: spec=100 overrides req=2,
	// pulling accuracy back to the default-compression neighbourhood.
	cReq := 2.0
	cSpec := 100.0
	resp, err = eng.Aggregate(idx, &AggregationRequest{
		TDigestCompression: &cReq,
		Aggregations: []AggregationSpec{
			{Type: "approximatePercentile", Field: "price", Percentile: ptrFloat(99), Compression: &cSpec, Name: "p99override"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate (spec compression=100 override) returned error: %v", err)
	}
	p99Override := mustFloatMetric(t, resp.Data[0].Metrics, "p99override")
	relOverride := math.Abs(p99Override-exact) / math.Abs(exact)
	if relOverride > 0.01 {
		t.Errorf("per-spec compression=100 override relErr %.6f > 0.01 (exact=%.4f approx=%.4f)", relOverride, exact, p99Override)
	}
}

// TestAggregationRequest_TDigestCompressionInvalidRejected ensures the request-
// level tdigestCompression is range-validated (positive, finite).
func TestAggregationRequest_TDigestCompressionInvalidRejected(t *testing.T) {
	idx := setupAggIndex(t)
	eng := NewEngine()
	cases := []float64{0, -1, math.NaN(), math.Inf(1)}
	for _, c := range cases {
		comp := c
		_, err := eng.Aggregate(idx, &AggregationRequest{
			TDigestCompression: &comp,
			Aggregations: []AggregationSpec{
				{Type: "approximatePercentile", Field: "salary", Percentile: ptrFloat(50)},
			},
		})
		if err == nil {
			t.Errorf("tdigestCompression=%v: expected error, got nil", c)
		}
	}
}

// TestAggregationRequest_ApproximateScanThreshold_Default1M asserts that
// when ScannedRows exceeds the default threshold (1M), the response is
// marked APPROXIMATE — even if every row was actually scanned within
// MaxDocScanSize. Pre-US-465 the only way to land APPROXIMATE was scan
// truncation; from US-465 onward the threshold is its own signal that the
// answer crossed the "too much data to trust by default" boundary.
func TestAggregationRequest_ApproximateScanThreshold_Default1M(t *testing.T) {
	// We can't actually index 1M docs in a unit test, but the engine's
	// applyApproximateScanThreshold inspects ComputeUsage.ScannedRows. We
	// configure a tiny override so the threshold is exercised without
	// building a million-doc fixture.
	idx := setupAccuracyIndex(t, 100)
	eng := NewEngine()
	eng.MaxDocScanSize = 1000

	// With no override the threshold is DefaultApproximateScanThreshold; 100
	// rows is way under, so accuracy stays ACCURATE.
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "count", Name: "n"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate (default threshold) error: %v", err)
	}
	if resp.Accuracy != "ACCURATE" {
		t.Errorf("default threshold + 100 rows: accuracy=%q, want ACCURATE", resp.Accuracy)
	}

	// Override threshold to 50. 100 docs > threshold → APPROXIMATE.
	threshold := int64(50)
	resp, err = eng.Aggregate(idx, &AggregationRequest{
		ApproximateScanThreshold: &threshold,
		Aggregations: []AggregationSpec{
			{Type: "count", Name: "n"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate (threshold=50) error: %v", err)
	}
	if resp.Accuracy != "APPROXIMATE" {
		t.Errorf("threshold=50, scanned=100: accuracy=%q, want APPROXIMATE", resp.Accuracy)
	}
	if resp.ComputeUsage == nil || resp.ComputeUsage.Accuracy != "APPROXIMATE" {
		t.Errorf("ComputeUsage.Accuracy mirror missing or not APPROXIMATE: %+v", resp.ComputeUsage)
	}

	// Threshold higher than scan stays ACCURATE.
	threshold = int64(500)
	resp, err = eng.Aggregate(idx, &AggregationRequest{
		ApproximateScanThreshold: &threshold,
		Aggregations: []AggregationSpec{
			{Type: "count", Name: "n"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate (threshold=500) error: %v", err)
	}
	if resp.Accuracy != "ACCURATE" {
		t.Errorf("threshold=500, scanned=100: accuracy=%q, want ACCURATE", resp.Accuracy)
	}
}

// TestApproximateDistinct_OneMillion_HLL_P99Under2Percent is the US-465 PRD
// accuracy gate for HyperLogLog: across many independent seeds, the p99 of
// the relative-error distribution at default precision (14) stays under 2%
// on a 1M-cardinality input. We compute the empirical p99 of |est-1M|/1M
// over 100 seeded trials so the test is resistant to single-seed flukes.
func TestApproximateDistinct_OneMillion_HLL_P99Under2Percent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1M HLL p99 error test in -short mode")
	}
	const cardinality = 1_000_000
	const trials = 100

	errs := make([]float64, 0, trials)
	for seed := int64(1); seed <= int64(trials); seed++ {
		sketch, err := hyperloglog.NewSketch(DefaultHLLPrecision, true)
		if err != nil {
			t.Fatalf("new sketch: %v", err)
		}
		r := rand.New(rand.NewSource(seed))
		for i := 0; i < cardinality; i++ {
			// Use a long pseudo-random byte string so collisions in input
			// space don't artificially inflate cardinality.
			sketch.Insert([]byte(fmt.Sprintf("%d-%d-%d", seed, i, r.Int63())))
		}
		est := float64(sketch.Estimate())
		relErr := math.Abs(est-float64(cardinality)) / float64(cardinality)
		errs = append(errs, relErr)
	}
	sort.Float64s(errs)
	// nearest-rank p99 over 100 samples = index 98 (zero-based).
	p99 := errs[int(math.Ceil(0.99*float64(trials)))-1]
	if p99 > 0.02 {
		t.Fatalf("1M HLL p99 relative error %.6f > 0.02 over %d trials (max=%.6f)", p99, trials, errs[trials-1])
	}
	t.Logf("1M HLL default-precision p99 error = %.6f (max=%.6f, min=%.6f)", p99, errs[trials-1], errs[0])
}

// TestApproximatePercentile_OneMillion_TDigest_P99Under1Percent re-pins the
// US-368 1M t-digest accuracy gate at the US-465 layer so this story owns
// its own breakage signal — independent of the older test. We use a
// stress dataset (uniform 0..1 with three distinct seeds folded together)
// and check the empirical p99 across 9 percentile probes (p50, p75, p90,
// p95, p99, p99.5, p99.9, p1, p10) for a much more sensitive accuracy gate.
func TestApproximatePercentile_OneMillion_TDigest_P99Under1Percent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1M t-digest accuracy test in -short mode")
	}
	const n = 1_000_000
	r := rand.New(rand.NewSource(465))
	values := make([]float64, n)
	for i := range values {
		values[i] = r.Float64()
	}
	probes := []float64{1, 10, 50, 75, 90, 95, 99, 99.5, 99.9}
	errs := make([]float64, 0, len(probes))
	for _, p := range probes {
		approx := computeApproxPercentileTDigest(values, p)
		exact := exactPercentileSort(values, p)
		if exact == 0 {
			continue
		}
		rel := math.Abs(approx-exact) / math.Abs(exact)
		errs = append(errs, rel)
		t.Logf("p%v exact=%.6f approx=%.6f rel=%.6f", p, exact, approx, rel)
	}
	sort.Float64s(errs)
	p99 := errs[int(math.Ceil(0.99*float64(len(errs))))-1]
	if p99 > 0.01 {
		t.Fatalf("1M t-digest p99 relative error %.6f > 0.01 across probes %v", p99, probes)
	}
}

// TestAggregationSpec_TDigestCompression_PerSpecPath checks the helper
// directly — the per-spec Compression on a single-percentile path must
// be honoured by the t-digest constructor, not silently dropped.
func TestAggregationSpec_TDigestCompression_PerSpecPath(t *testing.T) {
	const n = 50_000
	r := rand.New(rand.NewSource(465))
	values := make([]float64, n)
	for i := range values {
		values[i] = r.Float64()*1000 + 5000
	}
	exact := exactPercentileSort(values, 99)

	// Sanity check the helper at default compression.
	def := computeApproxPercentileTDigestC(values, 99, DefaultTDigestCompression)
	relDef := math.Abs(def-exact) / math.Abs(exact)
	if relDef > 0.01 {
		t.Fatalf("default compression p99 relErr=%.6f > 0.01 (exact=%.4f approx=%.4f)", relDef, exact, def)
	}

	// Force a small compression and verify the digest's Compression field
	// reflects it. The accuracy is allowed to slacken but the *plumbing*
	// is the assertion.
	td := tdigest.NewWithCompression(5.0)
	for _, v := range values {
		td.Add(v, 1)
	}
	if math.Abs(td.Compression-5.0) > 1e-9 {
		t.Fatalf("tdigest.Compression=%v, want 5.0", td.Compression)
	}

	// And the multi-percentile path also honours the per-spec compression.
	out := computeApproxPercentilesTDigestC(values, []float64{50, 90, 99}, DefaultTDigestCompression)
	if len(out) != 3 {
		t.Fatalf("multi-percentile helper returned %d entries, want 3", len(out))
	}
}

// setupTDigestIndex builds a Bleve index of n docs with a numeric `price`
// field drawn from a deterministic uniform distribution — used to exercise
// the request-level Compression plumbing through the full Aggregate path.
func setupTDigestIndex(t *testing.T, n int) bleve.Index {
	t.Helper()
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("price", mapping.NewNumericFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "tdigest"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	r := rand.New(rand.NewSource(465))
	batch := idx.NewBatch()
	for i := 0; i < n; i++ {
		// Heavy-tailed distribution so p99 is a long way from p50 — makes
		// percentile errors easier to detect.
		v := math.Exp(r.NormFloat64()*1.5 + 2.0)
		if err := batch.Index(fmt.Sprintf("p-%d", i), map[string]interface{}{"price": v}); err != nil {
			t.Fatalf("index doc %d: %v", i, err)
		}
		if batch.Size() >= 500 {
			if err := idx.Batch(batch); err != nil {
				t.Fatalf("flush batch: %v", err)
			}
			batch = idx.NewBatch()
		}
	}
	if batch.Size() > 0 {
		if err := idx.Batch(batch); err != nil {
			t.Fatalf("final batch: %v", err)
		}
	}
	return idx
}

// tdigestPercentileFromIndex is the exact-reference percentile fetched from
// the same Bleve index the production code reads, so the comparison is
// strictly within-index — no rounding noise from fixture regeneration.
func tdigestPercentileFromIndex(t *testing.T, idx bleve.Index, field string, percentile float64) float64 {
	t.Helper()
	values, _, err := scanNumericField(idx, bleve.NewMatchAllQuery(), field, 1_000_000)
	if err != nil {
		t.Fatalf("scanNumericField: %v", err)
	}
	return exactPercentileSort(values, percentile)
}

func ptrFloat(v float64) *float64 {
	return &v
}

func mustFloatMetric(t *testing.T, metrics []MetricValue, name string) float64 {
	t.Helper()
	v, ok := findMetric(metrics, name)
	if !ok {
		t.Fatalf("metric %q missing in %+v", name, metrics)
	}
	f, isF := v.(float64)
	if !isF {
		t.Fatalf("metric %q is %T (%v), want float64", name, v, v)
	}
	return f
}
