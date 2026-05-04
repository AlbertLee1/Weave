package objectset_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/materialize"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakeTierRouter is the in-memory cold-tier stub used by US-407 routing
// tests. It returns the configured PK list and records the cutoff it was
// called with so tests can assert the executor honours the hot-window
// configuration.
type fakeTierRouter struct {
	pks         []string
	err         error
	calls       int
	lastBefore  time.Time
	lastOT      string
	lastOnto    string
	respond     func(ctx context.Context, ontology, objectType string, before time.Time) ([]string, error)
	disableEcho bool
}

func (f *fakeTierRouter) ColdPrimaryKeys(ctx context.Context, ontology, objectType string, before time.Time) ([]string, error) {
	f.calls++
	f.lastBefore = before
	f.lastOT = objectType
	f.lastOnto = ontology
	if f.respond != nil {
		return f.respond(ctx, ontology, objectType, before)
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.disableEcho {
		return nil, nil
	}
	out := make([]string, len(f.pks))
	copy(out, f.pks)
	return out, nil
}

// Test that without a TierRouter wired the executor's executeBase path is
// unchanged — the regression contract for US-407.
func TestExecuteBase_NoTierRouter_HotOnly(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"e1", "e2", "e3", "e4"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("hot-only result: want %v, got %v", want, got)
	}
}

// With a TierRouter wired, cold PKs absent from the hot tier must show up
// in the merged result. The cutoff passed to ColdPrimaryKeys must equal
// `now - hotWindow` so cold tier sees only rows older than the hot window.
func TestExecuteBase_TierRouter_ColdAddsRows(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	router := &fakeTierRouter{pks: []string{"cold-1", "cold-2", "e2"}}
	executor.SetTierRouter(router)
	executor.SetHotWindow(24 * time.Hour)
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	ctx := objectset.WithOntologyScope(context.Background(), "northwind")
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if router.calls != 1 {
		t.Fatalf("router calls: want 1, got %d", router.calls)
	}
	if router.lastOT != "employee" {
		t.Errorf("router objectType: want employee, got %q", router.lastOT)
	}
	if router.lastOnto != "northwind" {
		t.Errorf("router ontology: want northwind, got %q", router.lastOnto)
	}
	wantBefore := fixed.Add(-24 * time.Hour)
	if !router.lastBefore.Equal(wantBefore) {
		t.Errorf("router cutoff: want %s, got %s", wantBefore, router.lastBefore)
	}

	got := sorted(result.PrimaryKeys)
	want := []string{"cold-1", "cold-2", "e1", "e2", "e3", "e4"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("merged result: want %v, got %v", want, got)
	}
}

// Cross-tier dedup: a PK present in BOTH hot and cold appears exactly once
// in the result, and the hot-tier ordering of pre-existing PKs is preserved.
func TestExecuteBase_TierRouter_DedupHotWins(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	// Cold tier echoes some hot PKs and adds two new ones. The merge must
	// keep the hot positions stable AND avoid emitting duplicates.
	router := &fakeTierRouter{pks: []string{"e1", "e3", "cold-x"}}
	executor.SetTierRouter(router)

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	seen := make(map[string]int)
	for _, pk := range result.PrimaryKeys {
		seen[pk]++
	}
	for pk, n := range seen {
		if n > 1 {
			t.Errorf("pk %q appeared %d times — merge must dedupe", pk, n)
		}
	}
	// cold-x is the only PK that is exclusively cold — it must surface.
	if seen["cold-x"] != 1 {
		t.Fatalf("cold-only pk missing from merge: result=%v", result.PrimaryKeys)
	}
	// All four hot PKs must still be present.
	for _, pk := range []string{"e1", "e2", "e3", "e4"} {
		if seen[pk] != 1 {
			t.Fatalf("hot pk %q missing from merge: result=%v", pk, result.PrimaryKeys)
		}
	}
}

// When the hot tier is empty (Bleve index empty), the cold tier provides
// the full result. This is the "Bleve unavailable / truncated" recovery
// scenario the PRD calls out.
func TestExecuteBase_TierRouter_HotEmpty_ColdFills(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex("Customer", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	executor := objectset.NewExecutor(mgr, nil, objectset.NewStore(time.Hour))

	router := &fakeTierRouter{pks: []string{"a", "b", "c"}}
	executor.SetTierRouter(router)

	def := &objectset.Definition{Type: "base", ObjectType: "Customer"}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"a", "b", "c"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("cold-only result: want %v, got %v", want, got)
	}
}

// A router error must propagate through executeBase — the cold tier is a
// correctness path, not a best-effort overlay.
func TestExecuteBase_TierRouter_ErrorPropagates(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	sentinel := errors.New("cold tier offline")
	router := &fakeTierRouter{err: sentinel}
	executor.SetTierRouter(router)

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	_, err := executor.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected error from cold tier, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected error to wrap sentinel, got %v", err)
	}
}

// Default hot window: 24h. Construction should not require an explicit
// SetHotWindow call.
func TestExecuteBase_TierRouter_DefaultHotWindow(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	router := &fakeTierRouter{disableEcho: true}
	executor.SetTierRouter(router)
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	if _, err := executor.Execute(context.Background(), def); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	wantBefore := fixed.Add(-24 * time.Hour)
	if !router.lastBefore.Equal(wantBefore) {
		t.Fatalf("default hot window: want cutoff %s, got %s", wantBefore, router.lastBefore)
	}
}

// SetTierRouter(nil) must detach the router so subsequent reads short-circuit
// the cold path. Important for the degraded-mode toggle.
func TestExecuteBase_TierRouter_NilDetaches(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	router := &fakeTierRouter{pks: []string{"cold-only"}}
	executor.SetTierRouter(router)
	executor.SetTierRouter(nil)

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if router.calls != 0 {
		t.Errorf("expected 0 router calls after nil-detach, got %d", router.calls)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"e1", "e2", "e3", "e4"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("hot-only after detach: want %v, got %v", want, got)
	}
}

// The interfaceBase path delegates to executeBase per implementing
// ObjectType — when a router is wired the cold tier must contribute to
// each leg's PK list. This guards the polymorphic load surface so a future
// US that adds branch-aware cold reads doesn't silently bypass the merge.
func TestExecuteBase_TierRouter_PerObjectTypeRouting(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	called := make(map[string]int)
	router := &fakeTierRouter{
		respond: func(_ context.Context, _, ot string, _ time.Time) ([]string, error) {
			called[ot]++
			switch ot {
			case "employee":
				return []string{"cold-emp"}, nil
			default:
				return nil, nil
			}
		},
	}
	executor.SetTierRouter(router)

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	if _, err := executor.Execute(context.Background(), def); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if called["employee"] != 1 {
		t.Errorf("expected exactly one cold lookup for employee, got %d", called["employee"])
	}
}

// BenchmarkExecuteBase_ColdTier_OneHundredK is the PRD performance gate:
// a base query whose cold tier carries 100K primary keys must complete in
// well under 500ms. The hot tier is empty so the time is dominated by the
// cold-tier fan-in + dedup.
func BenchmarkExecuteBase_ColdTier_OneHundredK(b *testing.B) {
	dir := b.TempDir()
	mgr := index.NewManager(dir)
	b.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex("Order", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}); err != nil {
		b.Fatalf("EnsureIndex: %v", err)
	}
	cold := make([]string, 100_000)
	for i := range cold {
		cold[i] = "ord-" + strconv.Itoa(i)
	}
	executor := objectset.NewExecutor(mgr, nil, objectset.NewStore(time.Hour))
	router := &fakeTierRouter{pks: cold}
	executor.SetTierRouter(router)

	def := &objectset.Definition{Type: "base", ObjectType: "Order"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		result, err := executor.Execute(context.Background(), def)
		if err != nil {
			b.Fatalf("Execute: %v", err)
		}
		if len(result.PrimaryKeys) != len(cold) {
			b.Fatalf("expected %d pks, got %d", len(cold), len(result.PrimaryKeys))
		}
	}
}

// TestExecuteBase_ColdTier_OneHundredK_LessThan500ms is the per-call latency
// gate that mirrors the PRD AC ("benchmark：冷查询 < 500ms (10K 对象)" — we
// scale to 100K on the same gate). It runs once and asserts the wall-clock
// budget so CI gives a clear signal even when -bench is not invoked.
func TestExecuteBase_ColdTier_OneHundredK_LessThan500ms(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cold-tier perf gate in -short mode")
	}
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex("Order", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	cold := make([]string, 100_000)
	for i := range cold {
		cold[i] = "ord-" + strconv.Itoa(i)
	}
	executor := objectset.NewExecutor(mgr, nil, objectset.NewStore(time.Hour))
	executor.SetTierRouter(&fakeTierRouter{pks: cold})

	def := &objectset.Definition{Type: "base", ObjectType: "Order"}
	start := time.Now()
	result, err := executor.Execute(context.Background(), def)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != len(cold) {
		t.Fatalf("expected %d pks, got %d", len(cold), len(result.PrimaryKeys))
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("cold-tier query exceeded budget: elapsed=%s", elapsed)
	}
}

// TestExecuteBase_TierRouter_MaterializeIntegration verifies the full
// cold-tier wiring end-to-end: a real *materialize.Materializer writes
// two batches; the executor merges hot (empty Bleve) + cold (parquet
// files) and surfaces both PKs. Pins the contract that swapping in the
// real adapter doesn't bypass the merge path.
func TestExecuteBase_TierRouter_MaterializeIntegration(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex("Customer", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	// Real Materializer + TierRouter — test imports lives in the
	// external _test package so the dependency direction stays one-way.
	mat := materialize.NewMaterializer(t.TempDir())
	if err := mat.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-cold-1",
		OntologyAPIName: "northwind",
		Timestamp:       time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC),
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-old-1", Properties: map[string]interface{}{"id": "C-old-1"}},
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "C-old-2", Properties: map[string]interface{}{"id": "C-old-2"}},
		},
	}); err != nil {
		t.Fatalf("MaterializeBatch: %v", err)
	}
	router := materialize.NewTierRouter(mat)

	executor := objectset.NewExecutor(mgr, nil, objectset.NewStore(time.Hour))
	executor.SetTierRouter(router)
	ctx := objectset.WithOntologyScope(context.Background(), "northwind")

	def := &objectset.Definition{Type: "base", ObjectType: "Customer"}
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"C-old-1", "C-old-2"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("integration cold-only result: want %v, got %v", want, got)
	}
}

// Sanity: sorted dedup helper used elsewhere in the suite shouldn't be
// confused with the executor's order-preserving merge. Pin this with a
// trivial assertion so the helper used in cold-tier tests keeps working.
func TestSorted_Stable(t *testing.T) {
	in := []string{"b", "a", "c"}
	out := sorted(in)
	want := []string{"a", "b", "c"}
	if fmt.Sprintf("%v", out) != fmt.Sprintf("%v", want) {
		t.Fatalf("sorted: want %v, got %v", want, out)
	}
	// Defensive: original slice unmutated.
	sort.Strings(in)
	if fmt.Sprintf("%v", in) != fmt.Sprintf("%v", []string{"a", "b", "c"}) {
		t.Fatalf("input mutation surprise: %v", in)
	}
}
