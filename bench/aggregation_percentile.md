# Aggregation — `approximatePercentile` Accuracy & Bench

Story: **US-018** (precision bench + 5% error assertion)
Source test file: `pkg/oss/aggregation/percentile_bench_test.go`

## Methodology

- Dataset: 10,000 points drawn from a Gaussian distribution with
  `mean=5000.0`, `stddev=1000.0`, clipped at `v >= 0`. Deterministic seed
  (`rand.NewSource(42)`), so numbers are reproducible across runs.
- Exact reference: `exactPercentileSort` — copy → `sort.Float64s` → nearest-rank
  index (`ceil(p/100 * n) - 1`).
- Approx implementation: `computeApproxPercentileHdr` on top of
  `github.com/HdrHistogram/hdrhistogram-go` with a `1e3` fixed-point scale
  (`percentileScale`) and shift to positive.
- Error metric: `|approx - exact| / |exact|`. The 5% bound (`rel <= 0.05`) is
  enforced as a hard `b.Fatalf` inside the benchmark AND as a `t.Fatalf` inside
  `TestApproxPercentileAccuracy_5PercentBound`, so regressions fail CI even
  when `-bench` is not supplied.

## Accuracy results (seed=42, n=10000)

| Percentile | exact        | approx       | relative error |
|------------|--------------|--------------|----------------|
| p50        | 5008.5782    | 5009.4070    | 0.000165       |
| p95        | 6645.7486    | 6647.8070    | 0.000310       |
| p99        | 7302.2972    | 7303.1670    | 0.000119       |

All three percentiles fall inside ≤0.05% relative error — two full orders of
magnitude below the 5% acceptance bound.

## Benchmark results

Run on Apple M3 Max, `go test -bench=Percentile -benchmem
./pkg/oss/aggregation/`:

| Benchmark                         | ns/op   | B/op   | allocs/op |
|-----------------------------------|---------|--------|-----------|
| `BenchmarkApproxPercentile/p50`   | 51,772  | 123008 | 2         |
| `BenchmarkApproxPercentile/p95`   | 52,425  | 123008 | 2         |
| `BenchmarkApproxPercentile/p99`   | 52,222  | 123008 | 2         |
| `BenchmarkApproxPercentileMulti`  | 70,468  | 123425 | 10        |
| `BenchmarkExactPercentileSort`    | 206,495 | 81937  | 1         |

Observations:

- The HdrHistogram-backed path is ~4x faster than `sort.Float64s` on 10k
  points even though it allocates a fresh histogram (~123 KB) each call.
- Multi-percentile single-pass (`BenchmarkApproxPercentileMulti`) amortises the
  histogram build across three percentiles and is ~1.34x the cost of a single
  percentile — well below the ~3x cost of three scalar calls.
- Allocation overhead is almost entirely the `hdrhistogram.New(...)` buffer.
  A future optimization could pool histograms across aggregation requests if
  percentile becomes a hot path (not required for US-018).

## Reproduction

```bash
# Accuracy assertion (fails CI if any percentile drifts past 5% relative error)
go test ./pkg/oss/aggregation/ -run TestApproxPercentileAccuracy_5PercentBound -v

# Full benchmark (same assertion enforced via b.Fatalf)
go test -bench=ApproxPercentile -benchmem ./pkg/oss/aggregation/

# Exact reference sort baseline
go test -bench=ExactPercentileSort -benchmem ./pkg/oss/aggregation/
```
