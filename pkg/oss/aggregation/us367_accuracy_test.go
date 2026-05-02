package aggregation

import (
	"fmt"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
)

// US-367 — accuracy=ALLOW_APPROXIMATE 走 HLL；REQUIRE_ACCURATE 走精确；
// 响应 accuracy 字段反映实际算法精度。

// setupUniqueIndex builds a Bleve index whose "tag" field has the requested
// number of distinct values (one per doc). Used to push the HLL sketch past
// its sparse→dense boundary so the routing tests have a deterministic dense
// regime to assert against.
func setupUniqueIndex(t *testing.T, distinct int) bleve.Index {
	t.Helper()
	indexMapping := bleve.NewIndexMapping()
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("tag", mapping.NewKeywordFieldMapping())
	indexMapping.DefaultMapping = docMapping

	dir := t.TempDir()
	idx, err := bleve.New(filepath.Join(dir, "unique"), indexMapping)
	if err != nil {
		t.Fatalf("create index: %v", err)
	}
	t.Cleanup(func() { idx.Close() })

	batch := idx.NewBatch()
	for i := 0; i < distinct; i++ {
		if err := batch.Index(fmt.Sprintf("doc-%d", i), map[string]interface{}{
			"tag": fmt.Sprintf("tag-%010d", i),
		}); err != nil {
			t.Fatalf("batch index: %v", err)
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

// Test the request-level Accuracy field defaults: a blank Accuracy is
// equivalent to ALLOW_APPROXIMATE, so existing callers keep their HLL path.
func TestAccuracyMode_DefaultIsAllowApproximate(t *testing.T) {
	idx := setupHLLIndex(t, 5000, 10000)
	eng := NewEngine()
	eng.MaxDocScanSize = 11000

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "tag", Name: "card"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	val, ok := findMetric(resp.Data[0].Metrics, "card")
	if !ok {
		t.Fatalf("missing 'card' metric")
	}
	got := val.(int)
	rel := math.Abs(float64(got-5000)) / 5000
	if rel > 0.02 {
		t.Errorf("default-accuracy HLL relative error %.4f > 2%%", rel)
	}
	if resp.Accuracy != "APPROXIMATE" {
		t.Errorf("response.accuracy = %q, want APPROXIMATE (HLL went dense at 5k cardinality)", resp.Accuracy)
	}
}

// Test that REQUIRE_ACCURATE on approximateDistinct routes to exact distinct
// AND surfaces ACCURATE on the response. The exact answer at 5000 distinct
// values is 5000, NOT the HLL estimate — so we assert byte-equality.
func TestAccuracyMode_RequireAccurate_DistinctRoutesToExact(t *testing.T) {
	idx := setupHLLIndex(t, 5000, 10000)
	eng := NewEngine()
	eng.MaxDocScanSize = 11000

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy: AccuracyRequireAccurate,
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "tag", Name: "card"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	val, ok := findMetric(resp.Data[0].Metrics, "card")
	if !ok {
		t.Fatalf("missing 'card' metric")
	}
	got := val.(int)
	if got != 5000 {
		t.Errorf("REQUIRE_ACCURATE distinct = %d, want 5000 (exact)", got)
	}
	if resp.Accuracy != "ACCURATE" {
		t.Errorf("response.accuracy = %q, want ACCURATE (REQUIRE_ACCURATE used exact path)", resp.Accuracy)
	}
}

// Test that ALLOW_APPROXIMATE on a small dataset still surfaces ACCURATE on
// the response, because the HLL sketch is in its sparse representation and
// the returned count is byte-exact. This protects callers that pre-date the
// HLL switch and relied on Bleve facets returning exact small-N counts.
func TestAccuracyMode_AllowApproximate_SmallCardinalityIsAccurate(t *testing.T) {
	idx := setupAggIndex(t) // 3 distinct departments
	eng := NewEngine()

	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy: AccuracyAllowApproximate,
		Aggregations: []AggregationSpec{
			{Type: "approximateDistinct", Field: "department", Name: "dept"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	val, ok := findMetric(resp.Data[0].Metrics, "dept")
	if !ok {
		t.Fatalf("missing 'dept' metric")
	}
	if val.(int) != 3 {
		t.Errorf("sparse HLL distinct = %v, want 3", val)
	}
	if resp.Accuracy != "ACCURATE" {
		t.Errorf("response.accuracy = %q, want ACCURATE (sparse HLL is exact)", resp.Accuracy)
	}
}

// Test that REQUIRE_ACCURATE on approximatePercentile routes through the
// exact sort-based percentile (not HdrHistogram) and reports ACCURATE.
func TestAccuracyMode_RequireAccurate_PercentileRoutesToExact(t *testing.T) {
	idx := setupAccuracyIndex(t, 100)
	eng := NewEngine()
	eng.MaxDocScanSize = 200

	p := 50.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy: AccuracyRequireAccurate,
		Aggregations: []AggregationSpec{
			{Type: "approximatePercentile", Field: "price", Percentile: &p, Name: "p50"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	val, ok := findMetric(resp.Data[0].Metrics, "p50")
	if !ok {
		t.Fatalf("missing 'p50' metric")
	}
	// Exact median of 1..100 (nearest-rank at p=50) is 50.
	got, ok := val.(float64)
	if !ok {
		t.Fatalf("expected float64 from exact percentile, got %T (%v)", val, val)
	}
	if got != 50.0 {
		t.Errorf("REQUIRE_ACCURATE p50 = %v, want 50.0 (exact)", got)
	}
	if resp.Accuracy != "ACCURATE" {
		t.Errorf("response.accuracy = %q, want ACCURATE", resp.Accuracy)
	}
}

// Test that ALLOW_APPROXIMATE on percentile still flips response.accuracy to
// APPROXIMATE because HdrHistogram is by definition an approximate algorithm,
// even when scan was not truncated.
func TestAccuracyMode_AllowApproximate_PercentileFlipsResponseToApproximate(t *testing.T) {
	idx := setupAccuracyIndex(t, 100)
	eng := NewEngine()
	eng.MaxDocScanSize = 200

	p := 50.0
	resp, err := eng.Aggregate(idx, &AggregationRequest{
		Accuracy: AccuracyAllowApproximate,
		Aggregations: []AggregationSpec{
			{Type: "approximatePercentile", Field: "price", Percentile: &p, Name: "p50"},
		},
	})
	if err != nil {
		t.Fatalf("Aggregate: %v", err)
	}
	if resp.Accuracy != "APPROXIMATE" {
		t.Errorf("response.accuracy = %q, want APPROXIMATE (HdrHistogram is approximate)", resp.Accuracy)
	}
}

// Test the 1M cardinality acceptance criterion at the sketch level (no
// Bleve scan): inserting 1M distinct strings into HLL p=14 should yield
// estimate within 2% of truth. This is the PRD threshold for US-367 and
// must hold independent of the storage stack.
func TestApproximateDistinct_OneMillion_RelativeErrorUnder2Percent(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1M HLL accuracy test in -short mode")
	}
	const cardinality = 1_000_000
	sketch, err := hyperloglog.NewSketch(DefaultHLLPrecision, true)
	if err != nil {
		t.Fatalf("new sketch: %v", err)
	}
	for i := 0; i < cardinality; i++ {
		sketch.Insert([]byte(fmt.Sprintf("uuid-%032d", i)))
	}
	estimate := int(sketch.Estimate())
	rel := math.Abs(float64(estimate-cardinality)) / cardinality
	if rel > 0.02 {
		t.Fatalf("1M HLL relative error %.4f > 2%% (estimate=%d, truth=%d)",
			rel, estimate, cardinality)
	}
	t.Logf("1M HLL estimate=%d truth=%d relative error=%.6f", estimate, cardinality, rel)
}

// TestApproximateDistinct_OneMillion_FasterThanExactByFiveX is the PRD perf
// gate: HLL must be ≥5× faster than the exact map[string]struct{} path on
// 1M distinct 32-byte UUID-like keys. Runs as a `go test` (not -bench) so
// CI catches regressions even when benchmarks are off; uses time.Now/Since
// rather than testing.B so the threshold is enforceable as t.Fatalf.
func TestApproximateDistinct_OneMillion_FasterThanExactByFiveX(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1M HLL vs exact perf test in -short mode")
	}
	const cardinality = 1_000_000
	keys := make([][]byte, cardinality)
	for i := 0; i < cardinality; i++ {
		keys[i] = []byte(fmt.Sprintf("uuid-%032d", i))
	}

	// Warm goroutine scheduler / page cache by touching the slice once.
	for _, k := range keys {
		_ = k[0]
	}

	exactStart := time.Now()
	exactSet := make(map[string]struct{}, cardinality)
	for _, k := range keys {
		exactSet[string(k)] = struct{}{}
	}
	exactDur := time.Since(exactStart)
	if len(exactSet) != cardinality {
		t.Fatalf("exact set populated to %d, want %d", len(exactSet), cardinality)
	}

	hllStart := time.Now()
	sketch, err := hyperloglog.NewSketch(DefaultHLLPrecision, true)
	if err != nil {
		t.Fatalf("new sketch: %v", err)
	}
	for _, k := range keys {
		sketch.Insert(k)
	}
	hllEstimate := int(sketch.Estimate())
	hllDur := time.Since(hllStart)

	rel := math.Abs(float64(hllEstimate-cardinality)) / cardinality
	if rel > 0.02 {
		t.Fatalf("HLL relative error %.4f > 2%%", rel)
	}

	speedup := float64(exactDur) / float64(hllDur)
	t.Logf("1M distinct: exact=%v hll=%v speedup=%.2f×", exactDur, hllDur, speedup)
	if speedup < 5.0 {
		t.Fatalf("HLL speedup %.2f× < 5× (exact=%v hll=%v)", speedup, exactDur, hllDur)
	}
}

// BenchmarkApproximateDistinct_HLL_OneMillion times sketch.Insert + Estimate
// over 1M distinct 32-byte UUIDs at default precision. Pair with
// BenchmarkExactDistinct_OneMillion to demonstrate the PRD ≥5× perf gate.
func BenchmarkApproximateDistinct_HLL_OneMillion(b *testing.B) {
	const cardinality = 1_000_000
	keys := make([][]byte, cardinality)
	for i := 0; i < cardinality; i++ {
		keys[i] = []byte(fmt.Sprintf("uuid-%032d", i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sketch, err := hyperloglog.NewSketch(DefaultHLLPrecision, true)
		if err != nil {
			b.Fatalf("new sketch: %v", err)
		}
		for _, k := range keys {
			sketch.Insert(k)
		}
		_ = sketch.Estimate()
	}
}

// BenchmarkExactDistinct_OneMillion is the reference cost the HLL path must
// beat by 5×. Uses map[string]struct{} which is the same algorithm
// computeExactDistinct runs under the hood.
func BenchmarkExactDistinct_OneMillion(b *testing.B) {
	const cardinality = 1_000_000
	keys := make([]string, cardinality)
	for i := 0; i < cardinality; i++ {
		keys[i] = fmt.Sprintf("uuid-%032d", i)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		set := make(map[string]struct{}, cardinality)
		for _, k := range keys {
			set[k] = struct{}{}
		}
		_ = len(set)
	}
}
