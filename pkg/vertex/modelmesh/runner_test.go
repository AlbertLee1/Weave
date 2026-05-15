package modelmesh_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/vertex/modelmesh"
)

func TestRunner_Given_LinearChain_When_Run_Then_OrderRespected(t *testing.T) {
	var mu sync.Mutex
	var order []string
	exec := func(_ context.Context, m modelmesh.ModelNode) error {
		mu.Lock()
		order = append(order, m.ID)
		mu.Unlock()
		return nil
	}
	models := []modelmesh.ModelNode{
		{ID: "m2", InputProperties: []string{"A"}, OutputProperties: []string{"B"}},
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m3", InputProperties: []string{"B"}},
	}
	r := &modelmesh.Runner{Concurrency: 4}
	results, err := r.Run(context.Background(), models, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if len(order) != 3 || order[0] != "m1" || order[1] != "m2" || order[2] != "m3" {
		t.Fatalf("expected exec order [m1 m2 m3], got %v", order)
	}
}

func TestRunner_Given_SameLayerSiblings_When_Run_Then_RunInParallel(t *testing.T) {
	siblingsInFlight := make(chan struct{})
	var entries int32

	exec := func(ctx context.Context, m modelmesh.ModelNode) error {
		if m.ID == "m2" || m.ID == "m3" {
			n := atomic.AddInt32(&entries, 1)
			if n == 2 {
				close(siblingsInFlight)
				return nil
			}
			select {
			case <-siblingsInFlight:
				return nil
			case <-time.After(2 * time.Second):
				return errors.New("sibling never started in parallel")
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	models := []modelmesh.ModelNode{
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m2", InputProperties: []string{"A"}, OutputProperties: []string{"B"}},
		{ID: "m3", InputProperties: []string{"A"}, OutputProperties: []string{"C"}},
	}

	r := &modelmesh.Runner{Concurrency: 4}
	results, err := r.Run(context.Background(), models, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for _, res := range results {
		if res.Err != nil {
			t.Fatalf("model %q failed: %v", res.ModelID, res.Err)
		}
	}
}

func TestRunner_Given_ModelFails_When_Run_Then_ErrorPropagatedAndDownstreamSkipped(t *testing.T) {
	boom := errors.New("boom")
	var executed []string
	var mu sync.Mutex
	exec := func(_ context.Context, m modelmesh.ModelNode) error {
		mu.Lock()
		executed = append(executed, m.ID)
		mu.Unlock()
		if m.ID == "m1" {
			return boom
		}
		return nil
	}
	models := []modelmesh.ModelNode{
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m2", InputProperties: []string{"A"}},
	}
	r := &modelmesh.Runner{Concurrency: 2}
	results, err := r.Run(context.Background(), models, exec)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("expected wrapped boom error, got %v", err)
	}
	if len(results) != 1 || results[0].ModelID != "m1" {
		t.Fatalf("expected only m1 result, got %v", results)
	}
	if len(executed) != 1 || executed[0] != "m1" {
		t.Fatalf("expected only m1 executed before short-circuit, got %v", executed)
	}
}

func TestRunner_Given_Cycle_When_Run_Then_ErrCycleDetected(t *testing.T) {
	models := []modelmesh.ModelNode{
		{ID: "m1", InputProperties: []string{"B"}, OutputProperties: []string{"A"}},
		{ID: "m2", InputProperties: []string{"A"}, OutputProperties: []string{"B"}},
	}
	r := &modelmesh.Runner{Concurrency: 4}
	exec := func(_ context.Context, _ modelmesh.ModelNode) error { return nil }
	_, err := r.Run(context.Background(), models, exec)
	if !errors.Is(err, modelmesh.ErrCycleDetected) {
		t.Fatalf("expected ErrCycleDetected, got %v", err)
	}
}

func TestRunner_Given_ConcurrencyLowerThanLayer_When_Run_Then_AllNodesEventuallyExecute(t *testing.T) {
	var executed sync.Map
	exec := func(_ context.Context, m modelmesh.ModelNode) error {
		executed.Store(m.ID, true)
		return nil
	}
	models := []modelmesh.ModelNode{
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m2", InputProperties: []string{"A"}},
		{ID: "m3", InputProperties: []string{"A"}},
		{ID: "m4", InputProperties: []string{"A"}},
		{ID: "m5", InputProperties: []string{"A"}},
	}
	r := &modelmesh.Runner{Concurrency: 1}
	if _, err := r.Run(context.Background(), models, exec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, id := range []string{"m1", "m2", "m3", "m4", "m5"} {
		if _, ok := executed.Load(id); !ok {
			t.Fatalf("expected %s to have executed", id)
		}
	}
}

// BDD #4 — benchmark guard: 3-model linear chain executes well under 3 s,
// proving the planner + worker-pool orchestration adds negligible overhead
// on top of the synchronous executor cost.
func TestRunner_Given_ThreeModelChain_When_Run_Then_CompletesUnder3Seconds(t *testing.T) {
	exec := func(_ context.Context, _ modelmesh.ModelNode) error {
		time.Sleep(50 * time.Millisecond)
		return nil
	}
	models := []modelmesh.ModelNode{
		{ID: "m1", OutputProperties: []string{"A"}},
		{ID: "m2", InputProperties: []string{"A"}, OutputProperties: []string{"B"}},
		{ID: "m3", InputProperties: []string{"B"}, OutputProperties: []string{"C"}},
	}
	r := &modelmesh.Runner{Concurrency: 4}
	start := time.Now()
	if _, err := r.Run(context.Background(), models, exec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dur := time.Since(start); dur >= 3*time.Second {
		t.Fatalf("3-model chain took %v, expected < 3s", dur)
	}
}

func TestRunner_Given_ResultsRecord_When_Run_Then_TimingPopulated(t *testing.T) {
	exec := func(_ context.Context, _ modelmesh.ModelNode) error {
		time.Sleep(5 * time.Millisecond)
		return nil
	}
	models := []modelmesh.ModelNode{
		{ID: "m1", OutputProperties: []string{"A"}},
	}
	r := &modelmesh.Runner{Concurrency: 1}
	results, err := r.Run(context.Background(), models, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Started.IsZero() {
		t.Fatalf("expected non-zero Started timestamp")
	}
	if !results[0].Completed.After(results[0].Started) {
		t.Fatalf("expected Completed > Started, got Started=%v Completed=%v", results[0].Started, results[0].Completed)
	}
}
