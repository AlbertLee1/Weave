package main

import (
	"strings"
	"testing"
)

func TestParseBench_TextFormat(t *testing.T) {
	input := `goos: darwin
goarch: arm64
pkg: github.com/liyang/weave/bench
cpu: Apple M3 Max
BenchmarkLoad_US441-16            	     142	   1704387 ns/op
BenchmarkAggregate_US441-16       	      81	   3239335 ns/op	    456 B/op	    7 allocs/op
BenchmarkMask_US441-16            	 1000000	       195.6 ns/op
PASS
ok  	github.com/liyang/weave/bench	1.234s
`
	got, err := parseBench(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseBench: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 benchmarks, got %d (%v)", len(got), got)
	}
	if g := got["BenchmarkLoad_US441"]; g.NsPerOp != 1704387 || g.Iterations != 142 {
		t.Errorf("Load: want ns=1704387 iter=142, got %+v", g)
	}
	if g := got["BenchmarkAggregate_US441"]; g.NsPerOp != 3239335 || g.Iterations != 81 {
		t.Errorf("Aggregate: want ns=3239335 iter=81, got %+v", g)
	}
	if g := got["BenchmarkMask_US441"]; g.NsPerOp != 195.6 || g.Iterations != 1000000 {
		t.Errorf("Mask: want ns=195.6 iter=1000000, got %+v", g)
	}
}

func TestParseBench_KeepsSlowestSampleAcrossCounts(t *testing.T) {
	input := `BenchmarkLoad_US441-16   100   200 ns/op
BenchmarkLoad_US441-16   100   500 ns/op
BenchmarkLoad_US441-16   100   300 ns/op
`
	got, err := parseBench(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseBench: %v", err)
	}
	if g := got["BenchmarkLoad_US441"]; g.NsPerOp != 500 {
		t.Errorf("max-of-3: want ns=500 (slowest), got %v", g.NsPerOp)
	}
}

func TestParseBench_IgnoresUnrelatedLines(t *testing.T) {
	input := `random log line
some other text
BenchmarkX-8   1   1000 ns/op
PASS
`
	got, err := parseBench(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseBench: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 bench, got %d", len(got))
	}
}

func TestEvaluate_PassFailRegression(t *testing.T) {
	measured := map[string]ResultEntry{
		"BenchmarkA": {NsPerOp: 100},
		"BenchmarkB": {NsPerOp: 130}, // 1.30x — over 1.20 threshold
		"BenchmarkC": {NsPerOp: 110}, // 1.10x — under 1.20 threshold
	}
	baseline := map[string]BaselineEntry{
		"BenchmarkA": {NsPerOp: 100},
		"BenchmarkB": {NsPerOp: 100},
		"BenchmarkC": {NsPerOp: 100},
	}
	results, regressions, missing := evaluate(measured, baseline, 1.20)

	if !results["BenchmarkA"].Pass {
		t.Errorf("A should pass at ratio=1.0")
	}
	if results["BenchmarkB"].Pass {
		t.Errorf("B should fail at ratio=1.3")
	}
	if !results["BenchmarkC"].Pass {
		t.Errorf("C should pass at ratio=1.1 with threshold=1.2")
	}
	if len(regressions) != 1 || regressions[0] != "BenchmarkB" {
		t.Errorf("regressions: want [BenchmarkB], got %v", regressions)
	}
	if len(missing) != 0 {
		t.Errorf("missing: want empty, got %v", missing)
	}
}

func TestEvaluate_BaselineMissingFromMeasurements(t *testing.T) {
	measured := map[string]ResultEntry{
		"BenchmarkA": {NsPerOp: 100},
	}
	baseline := map[string]BaselineEntry{
		"BenchmarkA": {NsPerOp: 100},
		"BenchmarkB": {NsPerOp: 100},
	}
	_, _, missing := evaluate(measured, baseline, 1.20)
	if len(missing) != 1 || missing[0] != "BenchmarkB" {
		t.Errorf("missing: want [BenchmarkB], got %v", missing)
	}
}

func TestEvaluate_NoBaselineEntryAlwaysPasses(t *testing.T) {
	measured := map[string]ResultEntry{
		"BenchmarkUnknown": {NsPerOp: 999_999},
	}
	results, regressions, _ := evaluate(measured, map[string]BaselineEntry{}, 1.20)
	if !results["BenchmarkUnknown"].Pass {
		t.Errorf("benchmark with no baseline should pass (treated as new): got fail")
	}
	if len(regressions) != 0 {
		t.Errorf("regressions: want empty, got %v", regressions)
	}
}

func TestEvaluate_ThresholdBoundaryInclusive(t *testing.T) {
	// At exactly threshold ratio (1.20x), the verdict must be PASS.
	measured := map[string]ResultEntry{
		"BenchmarkAtBoundary": {NsPerOp: 120},
	}
	baseline := map[string]BaselineEntry{
		"BenchmarkAtBoundary": {NsPerOp: 100},
	}
	results, regressions, _ := evaluate(measured, baseline, 1.20)
	if !results["BenchmarkAtBoundary"].Pass {
		t.Errorf("ratio=1.20 with threshold=1.20 should pass (inclusive)")
	}
	if len(regressions) != 0 {
		t.Errorf("no regressions expected at exact boundary")
	}
}
