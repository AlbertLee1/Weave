package bench

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/links"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// US-464 hardens the multi-hop searchAround performance gate at the PRD-
// declared scale: 10K seed objects walked across a 3-hop forward chain with
// cycle detection enabled. The companion P99 test below turns the PRD
// budget ("< 200ms P99") into a hard CI failure when the executor regresses.
//
// Why this lives in bench/ rather than pkg/oss/objectset:
//   - The pkg-local BenchmarkExecute_SearchAround_Path_ThreeHops_10K
//     (in searcharound_us366_test.go) measures one Execute call as a raw
//     Go benchmark and is useful for `go test -bench=...` ad hoc runs, but
//     it isn't wired to a hard latency budget assertion.
//   - bench/ is the cross-subsystem regression suite (see doc.go); putting
//     the 10K/3-hop gate here keeps the budget alongside the other US-441
//     latency contracts and lets future Phase 36 work pick up the same
//     fanout-shaped fixture.
//
// Fanout shape: e0..e9999 (employee, 10K seed PKs) → d0..d199 (department,
// 200 PKs after dedup) → p0..p49 (building, 50 PKs) → c0..c4 (city, 5 PKs).
// The shrinking cardinality at each hop forces dedupeStrings to fire
// between hops (so the bench measures the realistic hot path, not a
// pathological fanout) while still being deterministic.

const us464SeedSize = 10_000

// us464ThreeHopBudget is the PRD-declared P99 ceiling for a 10K-object /
// 3-hop searchAround. The implementation comfortably clears this on the
// recording machine (one Execute on a 10K seed is ~6-10 ms in practice);
// the budget is set so that a 20× regression flips this gate red rather
// than silently degrading SDK throughput.
const us464ThreeHopBudget = 200 * time.Millisecond

// BenchmarkSearchAround_ThreeHops_10K_US464 measures one Execute call over
// the 10K-seed / 3-hop fanout. It pairs with the P99 test below; running
// just the benchmark (e.g. `go test -bench=BenchmarkSearchAround_ThreeHops_10K_US464`)
// is useful when profiling executor changes.
func BenchmarkSearchAround_ThreeHops_10K_US464(b *testing.B) {
	def, executor := newUS464ThreeHopFixture(b)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := executor.Execute(ctx, def); err != nil {
			b.Fatalf("Execute: %v", err)
		}
	}
}

// TestSearchAround_ThreeHops_10K_US464_P99WithinBudget is the PRD US-464
// hard latency gate. It samples 25 Execute calls (kept small so the
// whole `go test ./bench/...` run stays in the ~few-seconds budget that
// the bench package targets) and fails if the 99th-percentile exceeds
// us464ThreeHopBudget. The threshold itself is generous because the
// measurement runs on an in-memory deterministic resolver — any non-
// trivial regression (e.g. a quadratic cycle-prune, removed dedup) will
// blow the budget by orders of magnitude.
func TestSearchAround_ThreeHops_10K_US464_P99WithinBudget(t *testing.T) {
	def, executor := newUS464ThreeHopFixture(t)
	ctx := context.Background()
	iters := 25 // runtime int so the float→int sample-index conversion below stays legal.

	samples := make([]time.Duration, iters)
	for i := 0; i < iters; i++ {
		start := time.Now()
		if _, err := executor.Execute(ctx, def); err != nil {
			t.Fatalf("Execute iter %d: %v", i, err)
		}
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
	p99 := samples[idx]
	if p99 > us464ThreeHopBudget {
		t.Fatalf("3-hop searchAround over %d seeds p99=%s exceeds US-464 budget %s",
			us464SeedSize, p99, us464ThreeHopBudget)
	}
	t.Logf("3-hop searchAround over %d seeds p99=%s (budget %s)",
		us464SeedSize, p99, us464ThreeHopBudget)
}

// newUS464ThreeHopFixture builds the deterministic 10K-seed / 3-hop
// resolver used by both the benchmark and the P99 gate. Sharing the fixture
// keeps the two measurements pointing at exactly the same workload.
func newUS464ThreeHopFixture(tb testing.TB) (*objectset.Definition, *objectset.Executor) {
	tb.Helper()
	mgr := index.NewManager(tb.TempDir())
	tb.Cleanup(func() { mgr.Close() })

	props := []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}
	for _, ot := range []string{"employee", "department", "building", "city"} {
		if _, err := mgr.EnsureIndex(ot, props); err != nil {
			tb.Fatalf("EnsureIndex %s: %v", ot, err)
		}
	}

	resolver := newUS464Resolver()
	seedPKs := make([]string, us464SeedSize)
	hop1 := make(map[string][]string, us464SeedSize)
	hop2 := make(map[string][]string, 200)
	hop3 := make(map[string][]string, 50)
	for i := 0; i < us464SeedSize; i++ {
		pk := fmt.Sprintf("e%d", i)
		seedPKs[i] = pk
		hop1[pk] = []string{fmt.Sprintf("d%d", i%200)}
	}
	for i := 0; i < 200; i++ {
		hop2[fmt.Sprintf("d%d", i)] = []string{fmt.Sprintf("p%d", i%50)}
	}
	for i := 0; i < 50; i++ {
		hop3[fmt.Sprintf("p%d", i)] = []string{fmt.Sprintf("c%d", i%5)}
	}
	resolver.addForward("employee", "worksInDept", "department", hop1)
	resolver.addForward("department", "housedInBuilding", "building", hop2)
	resolver.addForward("building", "locatedInCity", "city", hop3)

	executor := objectset.NewExecutor(mgr, resolver, objectset.NewStore(time.Hour))
	def := &objectset.Definition{
		Type: "searchAround",
		ObjectSet: &objectset.Definition{
			// Use "static" so the bench isolates path traversal (cycle
			// prune + dedup + fanout) from Bleve query overhead — the
			// PRD budget is for the executor, not the inner ObjectSet.
			Type:        "static",
			ObjectType:  "employee",
			PrimaryKeys: seedPKs,
		},
		Path: []objectset.PathStep{
			{Link: "worksInDept"},
			{Link: "housedInBuilding"},
			{Link: "locatedInCity"},
		},
	}
	return def, executor
}

// us464Resolver is a fanout-table LinkResolver dedicated to the US-464
// fixture. We don't reuse benchResolver from suite_test.go because that
// type indexes by single sourcePK (one slice per PK); the 10K-seed shape
// is more legible with a whole-map registration pass.
type us464Resolver struct {
	forward    map[string]map[string][]string
	targetType map[string]string
}

func newUS464Resolver() *us464Resolver {
	return &us464Resolver{
		forward:    map[string]map[string][]string{},
		targetType: map[string]string{},
	}
}

func (r *us464Resolver) addForward(sourceOT, link, targetOT string, edges map[string][]string) {
	r.forward[sourceOT+"|"+link] = edges
	r.targetType[sourceOT+"|"+link] = targetOT
}

func (r *us464Resolver) ResolveLinkedObjects(_ context.Context, _ string, _ []string) ([]string, error) {
	return nil, nil
}

func (r *us464Resolver) ResolveLinkedObjectsByAPIName(_ context.Context, sourceOT, link string, sourcePKs []string) ([]string, error) {
	edges, ok := r.forward[sourceOT+"|"+link]
	if !ok {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(sourcePKs))
	out := make([]string, 0, len(sourcePKs))
	for _, pk := range sourcePKs {
		for _, t := range edges[pk] {
			if _, ok := seen[t]; !ok {
				seen[t] = struct{}{}
				out = append(out, t)
			}
		}
	}
	return out, nil
}

func (r *us464Resolver) ResolveLinked(_ context.Context, _ string, _ []string, _ links.Direction) ([]string, error) {
	return nil, nil
}

func (r *us464Resolver) ResolveTargetObjectType(_ context.Context, sourceOT, link string) (string, error) {
	if t, ok := r.targetType[sourceOT+"|"+link]; ok {
		return t, nil
	}
	return "", fmt.Errorf("no target for %s|%s", sourceOT, link)
}
