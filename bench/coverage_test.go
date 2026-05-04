package bench

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// TestBaselineCoversAllSubsystems is a structural gate over bench/baseline.json
// — it asserts the file declares the eight US-441 benchmark names so a
// rename or accidental deletion of any subsystem's bench fails the unit
// suite even when the gate (`make bench`) hasn't been re-run. It does NOT
// inspect the recorded ns/op values; that's the runtime gate's job.
func TestBaselineCoversAllSubsystems(t *testing.T) {
	want := []string{
		"BenchmarkAction_US441",
		"BenchmarkAggregate_US441",
		"BenchmarkFunction_US441",
		"BenchmarkIndex_US441",
		"BenchmarkLoad_US441",
		"BenchmarkMask_US441",
		"BenchmarkRLS_US441",
		"BenchmarkSearchAround_US441",
	}

	bs, err := os.ReadFile("baseline.json")
	if err != nil {
		t.Fatalf("read baseline.json: %v", err)
	}
	var bf struct {
		Threshold  float64                       `json:"thresholdRatio"`
		Benchmarks map[string]map[string]float64 `json:"benchmarks"`
	}
	if err := json.Unmarshal(bs, &bf); err != nil {
		t.Fatalf("unmarshal baseline.json: %v", err)
	}
	if bf.Threshold <= 0 {
		t.Errorf("thresholdRatio must be > 0, got %v", bf.Threshold)
	}
	got := make([]string, 0, len(bf.Benchmarks))
	for n, row := range bf.Benchmarks {
		got = append(got, n)
		if row["nsPerOp"] <= 0 {
			t.Errorf("%s: nsPerOp must be > 0, got %v", n, row["nsPerOp"])
		}
	}
	sort.Strings(got)
	for _, w := range want {
		var ok bool
		for _, g := range got {
			if g == w {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("baseline.json is missing %q (declared benchmarks: %v)", w, got)
		}
	}
}
