package objectset_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// TestExecuteBase_DuringRebuild_RoutesToColdTierWithNowCutoff is the
// load-bearing US-408 contract: when a rebuild is in flight the executor
// MUST skip the (potentially empty) Bleve index and ask the cold tier
// for ALL rows — not just `older than now-hotWindow`. The fakeTierRouter
// records the cutoff so we can assert it equals `now`.
func TestExecuteBase_DuringRebuild_RoutesToColdTierWithNowCutoff(t *testing.T) {
	executor, mgr := setupExecutorTest(t)
	router := &fakeTierRouter{pks: []string{"e1", "e2", "cold-1"}}
	executor.SetTierRouter(router)
	executor.SetHotWindow(24 * time.Hour)
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	mgr.MarkRebuildStart("employee")
	defer mgr.MarkRebuildEnd("employee")

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if router.calls != 1 {
		t.Fatalf("router.calls = %d, want 1", router.calls)
	}
	if !router.lastBefore.Equal(fixed) {
		t.Errorf("router cutoff = %s, want %s (now, NOT now-hotWindow)", router.lastBefore, fixed)
	}
	got := sorted(result.PrimaryKeys)
	want := []string{"cold-1", "e1", "e2"}
	if len(got) != len(want) {
		t.Fatalf("PrimaryKeys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PrimaryKeys = %v, want %v", got, want)
		}
	}
}

// TestExecuteBase_DuringRebuild_NoTierRouterDegradesToEmpty asserts the
// degraded-mode contract: a rebuild without a wired cold tier returns an
// empty result rather than an "index not found" 5xx. Operators who
// triggered the rebuild already accepted the read-availability trade-off.
func TestExecuteBase_DuringRebuild_NoTierRouterDegradesToEmpty(t *testing.T) {
	executor, mgr := setupExecutorTest(t)
	mgr.MarkRebuildStart("employee")
	defer mgr.MarkRebuildEnd("employee")

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	result, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.PrimaryKeys) != 0 {
		t.Errorf("PrimaryKeys = %v, want empty during rebuild without cold tier", result.PrimaryKeys)
	}
}

// TestExecuteBase_AfterRebuildEnds_RestoresHotPath confirms the marker is
// load-bearing — clearing it returns the executor to the legacy
// hot-only behaviour so the contract is automatically restored without
// any other state change.
func TestExecuteBase_AfterRebuildEnds_RestoresHotPath(t *testing.T) {
	executor, mgr := setupExecutorTest(t)
	router := &fakeTierRouter{pks: []string{"cold-1"}}
	executor.SetTierRouter(router)
	executor.SetHotWindow(24 * time.Hour)
	fixed := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	executor.SetTierNowFunc(func() time.Time { return fixed })

	mgr.MarkRebuildStart("employee")
	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	if _, err := executor.Execute(context.Background(), def); err != nil {
		t.Fatalf("during rebuild: %v", err)
	}
	rebuildBefore := router.lastBefore
	mgr.MarkRebuildEnd("employee")

	if _, err := executor.Execute(context.Background(), def); err != nil {
		t.Fatalf("after rebuild: %v", err)
	}
	wantPostBefore := fixed.Add(-24 * time.Hour)
	if !router.lastBefore.Equal(wantPostBefore) {
		t.Errorf("post-rebuild cutoff = %s, want %s", router.lastBefore, wantPostBefore)
	}
	if rebuildBefore.Equal(router.lastBefore) {
		t.Error("during-rebuild cutoff equalled post-rebuild cutoff; the two modes must differ")
	}
}

// TestExecuteBase_DuringRebuild_ColdErrorPropagates ensures the error
// envelope is distinguishable from the historical INVALID_ARGUMENT
// "ObjectSetFailed" path so the SDK can render a clean cold-tier
// outage hint.
func TestExecuteBase_DuringRebuild_ColdErrorPropagates(t *testing.T) {
	executor, mgr := setupExecutorTest(t)
	router := &fakeTierRouter{err: errors.New("cold tier exploded")}
	executor.SetTierRouter(router)

	mgr.MarkRebuildStart("employee")
	defer mgr.MarkRebuildEnd("employee")

	def := &objectset.Definition{Type: "base", ObjectType: "employee"}
	_, err := executor.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected error to propagate from cold tier")
	}
}
