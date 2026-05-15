// Package vertexbench houses Vertex-specific performance regression guards.
// VTX-098 owns the Scenario Read Overlay fold gates: at 100/1000/10000 edits
// the per-fold p99 must stay below 20 ms / 100 ms / 1 s respectively. The
// thresholds are also archived in bench/results/vertex-overlay.md so future
// regressions are visible in the report rather than buried in test logs.
package vertexbench

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/scenarios"
)

func raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// makeEdits builds n modifyProperty edits that all target the same
// (Airport, JFK) object — the worst-case shape for FoldObject because every
// edit has to be replayed in seq order.
func makeEdits(n int) []scenarios.ScenarioEdit {
	edits := make([]scenarios.ScenarioEdit, n)
	for i := 0; i < n; i++ {
		edits[i] = scenarios.ScenarioEdit{
			Seq:        int64(i + 1),
			Op:         "modifyProperty",
			ObjectType: "Airport",
			ObjectID:   "JFK",
			Property:   "capacity",
			NewValue:   raw(100 + i),
		}
	}
	return edits
}

func makeBase() *scenarios.ObjectView {
	return &scenarios.ObjectView{
		ObjectType: "Airport",
		ObjectID:   "JFK",
		Properties: map[string]json.RawMessage{
			"capacity": raw(100),
			"name":     raw("John F Kennedy"),
		},
	}
}

// BenchmarkScenarioOverlay_Fold100 measures FoldObject over 100 edits.
func BenchmarkScenarioOverlay_Fold100(b *testing.B) { benchFold(b, 100) }

// BenchmarkScenarioOverlay_Fold1000 measures FoldObject over 1 000 edits.
func BenchmarkScenarioOverlay_Fold1000(b *testing.B) { benchFold(b, 1000) }

// BenchmarkScenarioOverlay_Fold10000 measures FoldObject over 10 000 edits.
func BenchmarkScenarioOverlay_Fold10000(b *testing.B) { benchFold(b, 10000) }

func benchFold(b *testing.B, n int) {
	target := scenarios.ObjectKey{ObjectType: "Airport", ObjectID: "JFK"}
	base := makeBase()
	edits := makeEdits(n)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, _ := scenarios.FoldObject(target, base, edits)
		if v == nil {
			b.Fatalf("FoldObject returned nil for n=%d", n)
		}
	}
}

// TestScenarioOverlay_Given_NEdits_When_Fold_Then_P99WithinBudget is the gate
// that turns the perf budgets in PRD VTX-098 into a hard test failure when
// fold regresses past 20 ms (100), 100 ms (1 000), or 1 s (10 000).
func TestScenarioOverlay_Given_NEdits_When_Fold_Then_P99WithinBudget(t *testing.T) {
	cases := []struct {
		n      int
		budget time.Duration
	}{
		{100, 20 * time.Millisecond},
		{1000, 100 * time.Millisecond},
		{10000, 1 * time.Second},
	}

	target := scenarios.ObjectKey{ObjectType: "Airport", ObjectID: "JFK"}
	base := makeBase()

	for _, c := range cases {
		c := c
		t.Run(fmt.Sprintf("n=%d", c.n), func(t *testing.T) {
			edits := makeEdits(c.n)
			p99 := measureP99(100, func() {
				v, _ := scenarios.FoldObject(target, base, edits)
				if v == nil {
					t.Fatalf("FoldObject returned nil for n=%d", c.n)
				}
			})
			if p99 > c.budget {
				t.Fatalf("FoldObject n=%d p99=%s exceeds budget %s", c.n, p99, c.budget)
			}
			t.Logf("FoldObject n=%d p99=%s (budget %s)", c.n, p99, c.budget)
		})
	}
}

// measureP99 runs fn iters times and returns the 99th-percentile latency.
// We rank samples by sort+index rather than streaming so the math stays
// obvious; iters stays small (≤100) so the cost is negligible.
func measureP99(iters int, fn func()) time.Duration {
	samples := make([]time.Duration, iters)
	for i := 0; i < iters; i++ {
		start := time.Now()
		fn()
		samples[i] = time.Since(start)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	idx := int(float64(iters)*0.99) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= iters {
		idx = iters - 1
	}
	return samples[idx]
}
