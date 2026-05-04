//go:build integration

package oms_test

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// US-437 — pgvector HNSW benchmark.
//
// The PRD demand is "10 万向量 < 50ms" against `vector(1536)` with the
// HNSW cosine index from migration 000011. Seeding 100K vectors at 1536
// dims and waiting for the HNSW build to settle takes minutes, so the
// 100K driver is opt-in via WEAVE_BENCH_KNN_100K=1; the default 10K
// driver runs whenever the pgvector image is on the host so the
// integration suite still exercises the kNN hot path.

const (
	knn100KEnv     = "WEAVE_BENCH_KNN_100K"
	knnDimensions  = 1536
	knnModel       = "weave-knn-bench-v1"
	knnObjectType  = "ri.ontology.main.object-type.knn-bench"
	knnK           = 10
	knnQueryRounds = 50
	knnP50CapMs    = 50.0
)

// randomUnitVector returns a deterministic pseudo-random 1536-dim vector
// drawn from a seeded PRNG. The output is L2-normalised so cosine
// distance stays well-defined and HNSW recall is stable across runs.
func randomUnitVector(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	var sumSq float64
	for i := range v {
		x := rng.NormFloat64()
		v[i] = float32(x)
		sumSq += x * x
	}
	norm := math.Sqrt(sumSq)
	if norm == 0 {
		return v
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

// seedKNNCorpus inserts n synthetic embeddings into the test database in
// batched chunks. Returns a slice of pre-computed query vectors so the
// bench loop rotates through them without per-iteration RNG cost.
func seedKNNCorpus(tb testing.TB, repo *oms.PGRepository, n int) [][]float32 {
	tb.Helper()
	ctx := context.Background()

	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xBADC0DE))

	const batch = 200
	start := time.Now()
	for i := 0; i < n; i += batch {
		end := i + batch
		if end > n {
			end = n
		}
		for j := i; j < end; j++ {
			e := &oms.ObjectEmbedding{
				ObjectTypeRID: knnObjectType,
				PrimaryKey:    "obj-" + strconv.Itoa(j),
				Embedding:     randomUnitVector(rng, knnDimensions),
				Model:         knnModel,
			}
			if err := repo.UpsertObjectEmbedding(ctx, e); err != nil {
				tb.Fatalf("seed embedding %d: %v", j, err)
			}
		}
		if i%5000 == 0 && i > 0 {
			tb.Logf("[knn-bench] seeded %d / %d (%s elapsed)", i, n, time.Since(start).Round(time.Millisecond))
		}
	}
	tb.Logf("[knn-bench] seeded %d vectors in %s", n, time.Since(start).Round(time.Millisecond))

	queries := make([][]float32, knnQueryRounds)
	for i := range queries {
		queries[i] = randomUnitVector(rng, knnDimensions)
	}
	return queries
}

// measureKNNLatency runs FindNearestNeighbors once per supplied query and
// returns the sorted wall-clock latencies in milliseconds. Includes
// network + parse overhead because that is what an SDK caller observes.
func measureKNNLatency(tb testing.TB, repo *oms.PGRepository, queries [][]float32) []float64 {
	tb.Helper()
	ctx := context.Background()
	samples := make([]float64, 0, len(queries))
	for _, q := range queries {
		t0 := time.Now()
		out, err := repo.FindNearestNeighbors(ctx, knnObjectType, q, knnK, knnModel)
		dur := time.Since(t0)
		if err != nil {
			tb.Fatalf("FindNearestNeighbors: %v", err)
		}
		if len(out) == 0 {
			tb.Fatalf("FindNearestNeighbors: empty result (corpus must contain rows)")
		}
		samples = append(samples, float64(dur.Microseconds())/1000.0)
	}
	sort.Float64s(samples)
	return samples
}

// percentile returns the q-th percentile (0..1) of an already-sorted
// slice using linear interpolation between adjacent samples — matches
// the influxdata/tdigest convention used elsewhere in the codebase so
// witnesses compare cleanly across feature areas.
func percentile(sortedSamples []float64, q float64) float64 {
	if len(sortedSamples) == 0 {
		return math.NaN()
	}
	if q <= 0 {
		return sortedSamples[0]
	}
	if q >= 1 {
		return sortedSamples[len(sortedSamples)-1]
	}
	pos := q * float64(len(sortedSamples)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sortedSamples[lo]
	}
	frac := pos - float64(lo)
	return sortedSamples[lo]*(1-frac) + sortedSamples[hi]*frac
}

// reportKNNLatency emits the canonical p50/p95/max line and (when run
// under *testing.B) calls b.ReportMetric so `go test -bench` consumers
// can machine-read the numbers.
func reportKNNLatency(tb testing.TB, label string, samples []float64) (p50, p95, mx float64) {
	tb.Helper()
	if len(samples) == 0 {
		tb.Fatalf("%s: zero samples", label)
	}
	p50 = percentile(samples, 0.50)
	p95 = percentile(samples, 0.95)
	mx = samples[len(samples)-1]
	tb.Logf("[knn-bench] %s n=%d p50=%.2fms p95=%.2fms max=%.2fms", label, len(samples), p50, p95, mx)
	if b, ok := tb.(*testing.B); ok {
		b.ReportMetric(p50, "p50ms")
		b.ReportMetric(p95, "p95ms")
		b.ReportMetric(mx, "maxms")
	}
	return p50, p95, mx
}

// TestPGRepository_FindNearestNeighbors_LatencyContract_HNSW_100K is the
// US-437 latency contract: 100K 1536-dim vectors → p50 < 50ms via the
// HNSW cosine index. Opt-in via WEAVE_BENCH_KNN_100K=1 because the seed
// alone (~600 MiB of float data + HNSW build) takes minutes — too slow
// for default `go test -tags integration` runs.
func TestPGRepository_FindNearestNeighbors_LatencyContract_HNSW_100K(t *testing.T) {
	if os.Getenv(knn100KEnv) != "1" {
		t.Skipf("%s=1 not set; skipping 100K kNN latency contract", knn100KEnv)
	}
	repo := setupRepo(t)
	if !pgvectorAvailable(t, repo) {
		t.Skip("pgvector extension not available; skipping kNN contract")
	}
	queries := seedKNNCorpus(t, repo, 100_000)

	samples := measureKNNLatency(t, repo, queries)
	p50, _, _ := reportKNNLatency(t, "100K contract", samples)
	if p50 > knnP50CapMs {
		t.Fatalf("US-437 latency contract violated: p50=%.2fms > %.2fms cap", p50, knnP50CapMs)
	}
}

// TestPGRepository_FindNearestNeighbors_LatencyContract_HNSW_10K is the
// scaled-down witness that runs whenever pgvector is available. It does
// NOT enforce the < 50ms cap (HNSW is faster on smaller corpora, but
// test infra adds non-trivial wall-clock overhead) — it exists so a
// regression in the kNN code path surfaces in CI even without the heavy
// 100K opt-in flag.
func TestPGRepository_FindNearestNeighbors_LatencyContract_HNSW_10K(t *testing.T) {
	repo := setupRepo(t)
	if !pgvectorAvailable(t, repo) {
		t.Skip("pgvector extension not available; skipping kNN bench")
	}
	queries := seedKNNCorpus(t, repo, 10_000)
	samples := measureKNNLatency(t, repo, queries)
	reportKNNLatency(t, "10K witness", samples)
}

// BenchmarkPGRepository_FindNearestNeighbors_HNSW_100K is the canonical
// `go test -bench` entry point. It runs `b.N` iterations of a kNN query
// against an N-row HNSW-indexed corpus; p50/p95/max metrics are emitted
// via ReportMetric so regression CI can read structured output.
//
// `WEAVE_BENCH_KNN_SIZE` overrides the default corpus size (100K) so a
// developer can probe smaller corpora without re-flashing the env var
// or recompiling.
func BenchmarkPGRepository_FindNearestNeighbors_HNSW_100K(b *testing.B) {
	repo := setupRepo(b)
	if !pgvectorAvailable(b, repo) {
		b.Skip("pgvector extension not available; skipping kNN bench")
	}

	n := 100_000
	if v := os.Getenv("WEAVE_BENCH_KNN_SIZE"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	queries := seedKNNCorpus(b, repo, n)
	if len(queries) == 0 {
		b.Fatal("no query vectors")
	}

	ctx := context.Background()
	b.ResetTimer()
	samples := make([]float64, 0, b.N)
	for i := 0; i < b.N; i++ {
		q := queries[i%len(queries)]
		t0 := time.Now()
		out, err := repo.FindNearestNeighbors(ctx, knnObjectType, q, knnK, knnModel)
		dur := time.Since(t0)
		if err != nil {
			b.Fatalf("FindNearestNeighbors: %v", err)
		}
		if len(out) == 0 {
			b.Fatalf("FindNearestNeighbors: empty result")
		}
		samples = append(samples, float64(dur.Microseconds())/1000.0)
	}
	sort.Float64s(samples)
	reportKNNLatency(b, fmt.Sprintf("bench n=%d", n), samples)
}
