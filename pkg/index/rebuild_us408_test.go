package index_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// TestManager_RebuildMarker_LifecycleSetsAndClears is the spine of US-408:
// the executor's hot-path probe needs IsRebuilding to flip true between
// MarkRebuildStart and MarkRebuildEnd, and to default false everywhere
// else. Empty keys are silently no-ops so callers don't need to guard.
func TestManager_RebuildMarker_LifecycleSetsAndClears(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	if mgr.IsRebuilding("northwind__Customer") {
		t.Fatal("default IsRebuilding should be false")
	}

	mgr.MarkRebuildStart("northwind__Customer")
	if !mgr.IsRebuilding("northwind__Customer") {
		t.Fatal("after MarkRebuildStart, IsRebuilding should be true")
	}
	if mgr.IsRebuilding("northwind__Order") {
		t.Fatal("marker should be per-scopedKey, not global")
	}

	mgr.MarkRebuildEnd("northwind__Customer")
	if mgr.IsRebuilding("northwind__Customer") {
		t.Fatal("after MarkRebuildEnd, IsRebuilding should be false")
	}

	// Empty key is a silent no-op.
	mgr.MarkRebuildStart("")
	mgr.MarkRebuildEnd("")
	if mgr.IsRebuilding("") {
		t.Fatal("empty key should never report rebuilding")
	}
}

// TestManager_RebuildMarker_MultipleScopedKeysIndependent confirms that
// concurrent rebuilds on different ObjectTypes don't shadow each other.
func TestManager_RebuildMarker_MultipleScopedKeysIndependent(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	mgr.MarkRebuildStart("a")
	mgr.MarkRebuildStart("b")
	mgr.MarkRebuildStart("c")
	if !mgr.IsRebuilding("a") || !mgr.IsRebuilding("b") || !mgr.IsRebuilding("c") {
		t.Fatal("all three keys should be marked")
	}
	mgr.MarkRebuildEnd("b")
	if !mgr.IsRebuilding("a") || mgr.IsRebuilding("b") || !mgr.IsRebuilding("c") {
		t.Fatal("end of b should not affect a or c")
	}
}

// TestManager_RebuildMarker_ConcurrentSafe stresses the RWMutex with
// many readers + a few writers to surface any data races. Run with -race.
func TestManager_RebuildMarker_ConcurrentSafe(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = mgr.IsRebuilding("k")
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				mgr.MarkRebuildStart("k")
				mgr.MarkRebuildEnd("k")
			}
		}()
	}
	wg.Wait()
	// Final state should be cleared (every Start has a matching End).
	if mgr.IsRebuilding("k") {
		t.Error("expected marker cleared after balanced Start/End pairs")
	}
}

// TestRebuildWithOptions_EmitsAllStages walks the full lifecycle and
// confirms every stage fires in order with sane Current / Total values.
func TestRebuildWithOptions_EmitsAllStages(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, src := newRebuildFixture()

	var events []index.ProgressEvent
	opts := index.RebuildOptions{
		Progress: func(ev index.ProgressEvent) {
			events = append(events, ev)
		},
	}
	res, err := index.RebuildWithOptions(context.Background(), mgr, repo, src,
		index.RebuildRequest{OntologyAPIName: "northwind", ObjectTypeAPIName: "Customer"},
		opts)
	if err != nil {
		t.Fatalf("RebuildWithOptions: %v", err)
	}
	if res.IndexedCount != 3 {
		t.Errorf("IndexedCount = %d, want 3", res.IndexedCount)
	}

	wantStages := []index.RebuildStage{
		index.RebuildStageStart,
		index.RebuildStageDrop,
		index.RebuildStageRecreate,
		index.RebuildStageEstimate,
		index.RebuildStageBatch,
		index.RebuildStageComplete,
	}
	if len(events) < len(wantStages) {
		t.Fatalf("got %d events: %+v, want at least %d", len(events), events, len(wantStages))
	}
	for i, want := range wantStages {
		if events[i].Stage != want {
			t.Errorf("event[%d].Stage = %q, want %q", i, events[i].Stage, want)
		}
	}

	// Estimate event must carry Total = doc count.
	if events[3].Total != 3 {
		t.Errorf("estimate Total = %d, want 3", events[3].Total)
	}

	// Final complete event reports final Current = 3 and Total = 3.
	last := events[len(events)-1]
	if last.Current != 3 || last.Total != 3 {
		t.Errorf("complete event Current=%d Total=%d, want 3/3", last.Current, last.Total)
	}
}

// TestRebuildWithOptions_RespectsBatchSize asserts that a small batch
// size produces multiple RebuildStageBatch events with cumulative
// counts.
func TestRebuildWithOptions_RespectsBatchSize(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo := &stubRebuildRepo{
		ontologies:  map[string]oms.Ontology{"o": {RID: "ri.o", APIName: "o"}},
		objectTypes: map[string]map[string]oms.ObjectType{"ri.o": {"T": {RID: "ri.t", OntologyRID: "ri.o", APIName: "T", PrimaryKey: "id"}}},
		properties: map[string][]oms.Property{
			"ri.t": {{APIName: "id", BaseType: "string", IsSearchable: true}},
		},
	}
	docs := make([]index.LatestDocument, 25)
	for i := range docs {
		pk := keyN(i)
		docs[i] = index.LatestDocument{PrimaryKey: pk, Body: map[string]interface{}{"id": pk}}
	}
	src := &stubDocSource{rows: map[string][]index.LatestDocument{"ri.t": docs}}

	var batchEvents []index.ProgressEvent
	_, err := index.RebuildWithOptions(context.Background(), mgr, repo, src,
		index.RebuildRequest{OntologyAPIName: "o", ObjectTypeAPIName: "T"},
		index.RebuildOptions{
			BatchSize: 10,
			Progress: func(ev index.ProgressEvent) {
				if ev.Stage == index.RebuildStageBatch {
					batchEvents = append(batchEvents, ev)
				}
			},
		})
	if err != nil {
		t.Fatalf("RebuildWithOptions: %v", err)
	}
	if len(batchEvents) != 3 {
		t.Fatalf("got %d batch events, want 3", len(batchEvents))
	}
	wantCurrents := []int{10, 20, 25}
	for i, want := range wantCurrents {
		if batchEvents[i].Current != want {
			t.Errorf("batch[%d].Current = %d, want %d", i, batchEvents[i].Current, want)
		}
		if batchEvents[i].Total != 25 {
			t.Errorf("batch[%d].Total = %d, want 25", i, batchEvents[i].Total)
		}
	}
}

// TestRebuildWithOptions_DefaultBatchSize confirms the BatchSize=0 default
// falls back to DefaultRebuildBatchSize (single batch for small inputs).
func TestRebuildWithOptions_DefaultBatchSize(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, src := newRebuildFixture()

	var batchCount int
	_, err := index.RebuildWithOptions(context.Background(), mgr, repo, src,
		index.RebuildRequest{OntologyAPIName: "northwind", ObjectTypeAPIName: "Customer"},
		index.RebuildOptions{
			Progress: func(ev index.ProgressEvent) {
				if ev.Stage == index.RebuildStageBatch {
					batchCount++
				}
			},
		})
	if err != nil {
		t.Fatalf("RebuildWithOptions: %v", err)
	}
	if batchCount != 1 {
		t.Errorf("expected 1 batch event for 3 docs at default size, got %d", batchCount)
	}
}

// TestRebuildWithOptions_MarkerSetDuringRebuild observes IsRebuilding
// from inside a progress callback to prove the marker is visible during
// the critical section, NOT just after MarkRebuildEnd has cleared it.
func TestRebuildWithOptions_MarkerSetDuringRebuild(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, src := newRebuildFixture()
	scopedKey := index.ScopedKey("northwind", "Customer")

	saw := make(map[index.RebuildStage]bool)
	_, err := index.RebuildWithOptions(context.Background(), mgr, repo, src,
		index.RebuildRequest{OntologyAPIName: "northwind", ObjectTypeAPIName: "Customer"},
		index.RebuildOptions{
			Progress: func(ev index.ProgressEvent) {
				if ev.Stage != index.RebuildStageStart {
					if !mgr.IsRebuilding(scopedKey) {
						t.Errorf("marker missing during stage %q", ev.Stage)
					}
				}
				saw[ev.Stage] = true
			},
		})
	if err != nil {
		t.Fatalf("RebuildWithOptions: %v", err)
	}
	if mgr.IsRebuilding(scopedKey) {
		t.Error("marker not cleared after rebuild")
	}
	if !saw[index.RebuildStageBatch] {
		t.Error("expected at least one batch stage")
	}
}

// TestRebuildWithOptions_MarkerClearedOnError confirms the defer-based
// cleanup runs even when the source returns an error mid-rebuild — the
// marker must not leak into subsequent reads.
func TestRebuildWithOptions_MarkerClearedOnError(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, _ := newRebuildFixture()
	badSrc := &stubDocSource{err: errors.New("io failure")}
	scopedKey := index.ScopedKey("northwind", "Customer")

	_, err := index.RebuildWithOptions(context.Background(), mgr, repo, badSrc,
		index.RebuildRequest{OntologyAPIName: "northwind", ObjectTypeAPIName: "Customer"},
		index.RebuildOptions{})
	if err == nil {
		t.Fatal("expected error from doc source")
	}
	if mgr.IsRebuilding(scopedKey) {
		t.Error("marker leaked after rebuild error")
	}
}

// TestRebuildWithOptions_NilSourceEmitsZeroEstimate confirms the nil
// source path still calls the progress hook through to estimate +
// complete so a CLI consumer doesn't hang waiting on a never-fires event.
func TestRebuildWithOptions_NilSourceEmitsZeroEstimate(t *testing.T) {
	mgr := index.NewManager(t.TempDir())
	defer mgr.Close()

	repo, _ := newRebuildFixture()

	var stages []index.RebuildStage
	_, err := index.RebuildWithOptions(context.Background(), mgr, repo, nil,
		index.RebuildRequest{OntologyAPIName: "northwind", ObjectTypeAPIName: "Customer"},
		index.RebuildOptions{
			Progress: func(ev index.ProgressEvent) {
				stages = append(stages, ev.Stage)
			},
		})
	if err != nil {
		t.Fatalf("RebuildWithOptions: %v", err)
	}
	wantTrailingTwo := []index.RebuildStage{index.RebuildStageEstimate, index.RebuildStageComplete}
	if len(stages) < 2 {
		t.Fatalf("got %d stages, want at least 2", len(stages))
	}
	got := stages[len(stages)-2:]
	for i, w := range wantTrailingTwo {
		if got[i] != w {
			t.Errorf("trailing[%d] = %q, want %q", i, got[i], w)
		}
	}
}

// keyN renders an integer as a zero-padded primary key so sorted comparisons
// are stable across the 25-doc fixture.
func keyN(i int) string {
	const digits = "0123456789"
	return string([]byte{'k', '_', digits[i/10], digits[i%10]})
}
