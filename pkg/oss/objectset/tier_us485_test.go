package objectset_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/materialize"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// US-485 — Parquet 冷存 + tier router. The cold-tier wiring is already in
// place from US-407/US-408; this story adds query-time-window detection so
// the executor can short-circuit the irrelevant tier when the request's
// declared time window falls wholly inside one tier. The hot/cold union
// path still runs for queries that straddle the boundary.
//
// PRD acceptance criteria covered here:
//   - Executor 检测 query 时间窗 → 路由热/冷
//   - 测试：跨界查询 union 热冷结果正确

// hotOnlyTime returns a TimeRangeHint anchored strictly inside the hot
// window (`now-3h .. now-1h` with the executor pinned at fixed and a 24h
// hot window). Used by every "skip cold" scenario.
func hotOnlyTime(fixed time.Time) *objectset.TimeRangeHint {
	from := fixed.Add(-3 * time.Hour)
	to := fixed.Add(-1 * time.Hour)
	return &objectset.TimeRangeHint{From: &from, To: &to}
}

// coldOnlyTime returns a TimeRangeHint anchored strictly inside the cold
// window (`now-72h .. now-48h`). Used by every "skip hot" scenario.
func coldOnlyTime(fixed time.Time) *objectset.TimeRangeHint {
	from := fixed.Add(-72 * time.Hour)
	to := fixed.Add(-48 * time.Hour)
	return &objectset.TimeRangeHint{From: &from, To: &to}
}

// crossWindowTime returns a TimeRangeHint that straddles the hot/cold
// boundary (`now-48h .. now-12h`). The exact boundary is at `now-24h` for
// the default hot window so both halves must execute.
func crossWindowTime(fixed time.Time) *objectset.TimeRangeHint {
	from := fixed.Add(-48 * time.Hour)
	to := fixed.Add(-12 * time.Hour)
	return &objectset.TimeRangeHint{From: &from, To: &to}
}

// TestUS485_HotOnlyTimeWindow_SkipsColdTier — a query whose window is
// strictly inside `[now-hotWindow, now]` MUST NOT call the cold tier.
// Routing the request through cold for hot-tier-resident data wastes IO
// and inflates latency; PRD AC explicitly requires "detect 时间窗 → 路由
// 热/冷", and the negative path (no cold call) is the load-bearing half.
func TestUS485_HotOnlyTimeWindow_SkipsColdTier(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	router := &fakeTierRouter{pks: []string{"cold-1", "cold-2"}}
	executor.SetTierRouter(router)
	executor.SetHotWindow(24 * time.Hour)
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	def := &objectset.Definition{
		Type:       "base",
		ObjectType: "employee",
		TimeRange:  hotOnlyTime(fixed),
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if router.calls != 0 {
		t.Fatalf("router.calls = %d, want 0 (hot-only window must skip cold)", router.calls)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"e1", "e2", "e3", "e4"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("hot-only PKs: want %v, got %v", want, got)
	}
}

// TestUS485_ColdOnlyTimeWindow_SkipsHotTier — a query whose window is
// strictly older than `now-hotWindow` MUST short-circuit the hot tier:
// Bleve cannot contain rows that fall fully into the cold partition by
// definition, so paying for the index search is pure waste. The cold
// cutoff sent to the router is the upper bound of the query window so the
// router does not over-return rows that fall outside the request.
func TestUS485_ColdOnlyTimeWindow_SkipsHotTier(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	router := &fakeTierRouter{pks: []string{"cold-A", "cold-B"}}
	executor.SetTierRouter(router)
	executor.SetHotWindow(24 * time.Hour)
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	tr := coldOnlyTime(fixed)
	def := &objectset.Definition{
		Type:       "base",
		ObjectType: "employee",
		TimeRange:  tr,
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if router.calls != 1 {
		t.Fatalf("router.calls = %d, want 1 (cold-only must route to cold)", router.calls)
	}
	// Cold cutoff must equal the request's upper bound — not the rolling
	// hot-window boundary — so the router can clip rows past the request
	// window before they reach the executor.
	if !router.lastBefore.Equal(*tr.To) {
		t.Errorf("router cutoff: want %s, got %s", *tr.To, router.lastBefore)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"cold-A", "cold-B"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("cold-only PKs: want %v, got %v (hot result %s must NOT leak in)",
			want, got, "{e1..e4}")
	}
}

// TestUS485_CrossWindowQuery_UnionsHotAndCold — the load-bearing case:
// the request window straddles `now-hotWindow`, so hot returns recent
// rows and cold returns historical rows. The merged result is the union
// of the two streams (PRD literal: 跨界查询 union 热冷结果正确).
func TestUS485_CrossWindowQuery_UnionsHotAndCold(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	router := &fakeTierRouter{pks: []string{"cold-old-1", "cold-old-2"}}
	executor.SetTierRouter(router)
	executor.SetHotWindow(24 * time.Hour)
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	tr := crossWindowTime(fixed)
	def := &objectset.Definition{
		Type:       "base",
		ObjectType: "employee",
		TimeRange:  tr,
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if router.calls != 1 {
		t.Fatalf("router.calls = %d, want 1 (cross window must visit cold)", router.calls)
	}
	// Cross-window cutoff is `now-hotWindow`: cold may return everything
	// older than the hot boundary, the hot side handles the rest.
	wantBefore := fixed.Add(-24 * time.Hour)
	if !router.lastBefore.Equal(wantBefore) {
		t.Errorf("cross-window cutoff: want %s, got %s", wantBefore, router.lastBefore)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"cold-old-1", "cold-old-2", "e1", "e2", "e3", "e4"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("cross-window union: want %v, got %v", want, got)
	}
}

// TestUS485_OpenEndedTimeRange_BehavesLikeCrossWindow — pointer-typed
// From/To make either side optional. A nil From means "since beginning of
// time" and a nil To means "up to now"; either case widens the request to
// at least one side of the hot boundary and the executor must serve from
// both tiers. Pinning this prevents a regression where the routing
// classifier assumes both bounds are set.
func TestUS485_OpenEndedTimeRange_BehavesLikeCrossWindow(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	router := &fakeTierRouter{pks: []string{"cold-open"}}
	executor.SetTierRouter(router)
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	to := fixed.Add(-12 * time.Hour) // ends inside hot window, From open ⇒ cross
	def := &objectset.Definition{
		Type:       "base",
		ObjectType: "employee",
		TimeRange:  &objectset.TimeRangeHint{To: &to},
	}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if router.calls != 1 {
		t.Fatalf("router.calls = %d, want 1 for open-ended-from cross window", router.calls)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"cold-open", "e1", "e2", "e3", "e4"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("open-ended cross-window union: want %v, got %v", want, got)
	}
}

// TestUS485_NoTimeRange_LegacyMergePathPreserved — when the request
// carries no time-window hint the executor MUST keep the US-407 behaviour
// of merging both tiers. This is the backwards-compatibility gate that
// keeps every existing caller (pagination, aggregation, etc.) untouched.
func TestUS485_NoTimeRange_LegacyMergePathPreserved(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	router := &fakeTierRouter{pks: []string{"cold-legacy"}}
	executor.SetTierRouter(router)
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if router.calls != 1 {
		t.Fatalf("router.calls = %d, want 1 (legacy path always merges)", router.calls)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"cold-legacy", "e1", "e2", "e3", "e4"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("legacy merge: want %v, got %v", want, got)
	}
}

// TestUS485_CrossWindow_RealMaterializer_UnionRoundTrip — wires the real
// parquet writer + tier router through a cross-window definition and
// asserts cold rows materialised into Parquet show up alongside the hot
// Bleve rows in the merged result. This is the PRD AC "跨界查询 union 热
// 冷结果正确" pinned end-to-end through real disk I/O.
func TestUS485_CrossWindow_RealMaterializer_UnionRoundTrip(t *testing.T) {
	dir := t.TempDir()
	mgr := index.NewManager(dir)
	t.Cleanup(func() { mgr.Close() })
	if _, err := mgr.EnsureIndex("Customer", []index.Property{{APIName: "id", BaseType: "string", IsSearchable: true}}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	// Hot rows (Bleve).
	for _, pk := range []string{"hot-1", "hot-2"} {
		if err := mgr.IndexDocument("Customer", pk, map[string]interface{}{"id": pk}); err != nil {
			t.Fatalf("IndexDocument %s: %v", pk, err)
		}
	}

	// Cold rows (Parquet via real materialize.Materializer).
	mat := materialize.NewMaterializer(t.TempDir())
	if err := mat.MaterializeBatch(context.Background(), funnel.EditBatch{
		ID:              "tx-us485-cold",
		OntologyAPIName: "northwind",
		Timestamp:       time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC),
		Edits: []funnel.Edit{
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "cold-1", Properties: map[string]interface{}{"id": "cold-1"}},
			{Type: funnel.EditTypeCreate, ObjectType: "Customer", PrimaryKey: "cold-2", Properties: map[string]interface{}{"id": "cold-2"}},
		},
	}); err != nil {
		t.Fatalf("MaterializeBatch: %v", err)
	}

	executor := objectset.NewExecutor(mgr, nil, objectset.NewStore(time.Hour))
	executor.SetTierRouter(materialize.NewTierRouter(mat))
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	tr := crossWindowTime(fixed)
	def := &objectset.Definition{
		Type:       "base",
		ObjectType: "Customer",
		TimeRange:  tr,
	}
	ctx := objectset.WithOntologyScope(context.Background(), "northwind")
	result, err := executor.Execute(ctx, def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"cold-1", "cold-2", "hot-1", "hot-2"}
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		t.Fatalf("cross-window real-materializer union: want %v, got %v", want, got)
	}
}
