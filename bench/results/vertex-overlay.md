# Vertex Scenario Read Overlay Benchmark (VTX-098)

`bench/vertex/scenario_overlay_bench_test.go` exercises `pkg/scenarios.FoldObject`
over a worst-case shape: every edit targets the same object and is a
`modifyProperty` that overwrites the same property. This forces the folder to
walk every edit (no early-exit), which is the regression surface the PRD cares
about.

## Performance budgets (PRD VTX-098)

| Edits  | p99 budget |
| -----: | ---------: |
| 100    | 20 ms      |
| 1 000  | 100 ms     |
| 10 000 | 1 s        |

Budgets are enforced as a hard assertion in
`TestScenarioOverlay_Given_NEdits_When_Fold_Then_P99WithinBudget` — a CI run
that overshoots any row fails the build.

## Latest run

Captured on Apple M3 Max (arm64), Go 1.22, `-benchtime=1s` against the in-tree
`FoldObject`. p99 was measured separately by the gate test (100 samples per
row, rank-then-index).

| Benchmark                              | ns/op   | observed p99 | budget |
| -------------------------------------- | ------: | -----------: | -----: |
| BenchmarkScenarioOverlay_Fold100-16    | 4 046   | ~109 µs      | 20 ms  |
| BenchmarkScenarioOverlay_Fold1000-16   | 45 536  | ~679 µs      | 100 ms |
| BenchmarkScenarioOverlay_Fold10000-16  | 531 599 | ~2.45 ms     | 1 s    |

All three rows clear their budget by more than two orders of magnitude on
current hardware, leaving plenty of headroom for the cold-cache and
contention surcharge that the gate test does not model.

## How to re-run

```bash
# Hard gate (fails CI on regression)
go test ./bench/vertex/... -run TestScenarioOverlay -v

# Raw nanosecond numbers
go test -run='^$' -bench=BenchmarkScenarioOverlay -benchtime=1s ./bench/vertex/...
```

When refreshing this report, append a new "Latest run" block (do not overwrite
prior runs) so regressions are visible in `git log` against this file.
