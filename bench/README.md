# Performance Benchmark Suite (US-441)

Eight cross-subsystem regression benchmarks gated by `cmd/benchcheck` against
`bench/baseline.json`. CI fails when any single benchmark regresses by more
than `thresholdRatio` (default **1.20** = 20%) versus the baseline.

## Areas covered

| Benchmark | Subsystem | Hot path |
|---|---|---|
| `BenchmarkLoad_US441` | OSS executor | `objectset.Executor.Execute(base)` over a 200-doc Bleve index |
| `BenchmarkAggregate_US441` | OSS aggregation | `aggregation.Engine.Aggregate` (avg + sum + count) over 200 docs |
| `BenchmarkSearchAround_US441` | OSS executor | 2-hop searchAround with cycle detection |
| `BenchmarkAction_US441` | actions | `ValidateParameters` + `CollapseEdits` round-trip |
| `BenchmarkFunction_US441` | Goja runtime | `functions.Runtime.Execute` over a small JS aggregator |
| `BenchmarkMask_US441` | cellsec | `cellsec.Engine.CompileForRow` per-row dispatch |
| `BenchmarkRLS_US441` | security | `security.CELEvaluator.EvaluateRuleSet` warm-cache hit |
| `BenchmarkIndex_US441` | index | `index.Manager.IndexDocument` + `Search` |

Each bench is intentionally lightweight (≤200 seed docs, in-memory stores)
so the full suite finishes in ~1 minute on a developer machine. Heavier
integration benchmarks live alongside their packages — see e.g.
`pkg/oms/embedding_us437_benchmark_test.go` for the 100K pgvector kNN
contract.

## Running

```bash
# Gate (fails if any bench regresses past the threshold)
make bench

# Refresh the baseline after an intentional perf change (commit the diff!)
make bench-update

# Override the per-benchmark wall-clock budget
make bench BENCH_TIME=500ms
```

`bench/results.json` is written on every `make bench` invocation with the
per-bench measurements + verdict; `bench/baseline.json` is only touched by
`make bench-update`.

## Baseline portability

`baseline.json` is recorded on the maintainer's local machine. CI runners
on different hardware will need to refresh the baseline once on a known-good
build:

```bash
make bench-update BENCH_TIME=200ms
git add bench/baseline.json
git commit -m "bench: refresh baseline on CI runner"
```

The 20% threshold is calibrated to absorb run-to-run jitter on the
recording machine; cross-machine variance (typically 2–3× between Apple
Silicon and x86 Linux) is NOT absorbed. Always refresh the baseline on the
machine that will be running the gate.
