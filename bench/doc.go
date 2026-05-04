// Package bench houses the cross-subsystem performance benchmark suite
// required by US-441. The suite covers eight canonical hot paths — load,
// aggregate, searchAround, action, function, mask, rls, and index — and is
// driven by `cmd/benchcheck` against `bench/baseline.json` to fail CI when
// any single benchmark regresses by more than 20% versus the recorded
// baseline.
//
// The benchmarks are intentionally lightweight (≤10K seed docs, in-memory
// stores, no network) so the full suite finishes in a few seconds; they
// trade absolute production-fidelity for tight CI feedback. Heavier
// integration benchmarks live alongside their packages (e.g. the 100K
// pgvector kNN bench in pkg/oms/embedding_us437_benchmark_test.go).
package bench
