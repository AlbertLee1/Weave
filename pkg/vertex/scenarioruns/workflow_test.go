package scenarioruns_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/vertex/scenarioruns"
)

// memPersister captures every checkpoint so tests can assert the
// progression of the run state.
type memPersister struct {
	mu          sync.Mutex
	checkpoints []scenarioruns.RunCheckpoint
	failOnIdx   int // 0 = never; 1 = fail on first save; etc.
	failErr     error
}

func (m *memPersister) SaveCheckpoint(_ context.Context, cp scenarioruns.RunCheckpoint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOnIdx > 0 && len(m.checkpoints)+1 == m.failOnIdx {
		return m.failErr
	}
	cpCopy := cp
	cpCopy.Completed = append([]string(nil), cp.Completed...)
	cpCopy.AttemptsByID = make(map[string]int, len(cp.AttemptsByID))
	for k, v := range cp.AttemptsByID {
		cpCopy.AttemptsByID[k] = v
	}
	m.checkpoints = append(m.checkpoints, cpCopy)
	return nil
}

func (m *memPersister) snapshot() []scenarioruns.RunCheckpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]scenarioruns.RunCheckpoint, len(m.checkpoints))
	copy(out, m.checkpoints)
	return out
}

func (m *memPersister) latest() scenarioruns.RunCheckpoint {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.checkpoints) == 0 {
		return scenarioruns.RunCheckpoint{}
	}
	return m.checkpoints[len(m.checkpoints)-1]
}

func TestWorkflow_Given_3Models2Actions_When_Execute_Then_AllRunInLayerOrder(t *testing.T) {
	var mu sync.Mutex
	var order []string
	exec := func(_ context.Context, a scenarioruns.Activity) error {
		mu.Lock()
		order = append(order, a.ID)
		mu.Unlock()
		return nil
	}
	persist := &memPersister{}
	wf := &scenarioruns.Workflow{
		Persist: persist,
		Sleep:   func(time.Duration) {},
	}

	activities := []scenarioruns.Activity{
		{ID: "m1", Kind: scenarioruns.ActivityKindModel, Layer: 0},
		{ID: "m2", Kind: scenarioruns.ActivityKindModel, Layer: 1},
		{ID: "m3", Kind: scenarioruns.ActivityKindModel, Layer: 1},
		{ID: "a1", Kind: scenarioruns.ActivityKindAction, Layer: 2},
		{ID: "a2", Kind: scenarioruns.ActivityKindAction, Layer: 2},
	}

	cp, results, err := wf.Execute(context.Background(), "run1", "scn1", activities, exec)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if cp.Status != scenarioruns.RunStatusSucceeded {
		t.Fatalf("status: got %q want succeeded", cp.Status)
	}
	if len(results) != 5 {
		t.Fatalf("results: got %d want 5", len(results))
	}
	if len(order) != 5 || order[0] != "m1" {
		t.Fatalf("order: got %v want layer 0 first (m1)", order)
	}
	// Layer 1 siblings (m2, m3) deterministic alphabetical.
	if !(order[1] == "m2" && order[2] == "m3") {
		t.Fatalf("layer 1 order: got %v want [m2 m3]", order[1:3])
	}
	// Layer 2 siblings (a1, a2) deterministic alphabetical.
	if !(order[3] == "a1" && order[4] == "a2") {
		t.Fatalf("layer 2 order: got %v want [a1 a2]", order[3:])
	}
	if len(cp.Completed) != 5 {
		t.Fatalf("completed: got %d want 5", len(cp.Completed))
	}
}

func TestWorkflow_Given_ActivityFailsAllRetries_When_Execute_Then_RunMarkedFailedAndRetried3Times(t *testing.T) {
	var attempts int32
	boom := errors.New("boom")
	exec := func(_ context.Context, _ scenarioruns.Activity) error {
		atomic.AddInt32(&attempts, 1)
		return boom
	}
	persist := &memPersister{}
	wf := &scenarioruns.Workflow{
		Policy:  scenarioruns.RetryPolicy{MaxAttempts: 3, BackoffMs: 0},
		Persist: persist,
		Sleep:   func(time.Duration) {},
	}
	activities := []scenarioruns.Activity{
		{ID: "a1", Kind: scenarioruns.ActivityKindAction, Layer: 0},
	}
	cp, results, err := wf.Execute(context.Background(), "run1", "scn1", activities, exec)
	if err == nil {
		t.Fatal("expected error")
	}
	if cp.Status != scenarioruns.RunStatusFailed {
		t.Fatalf("status: got %q want failed", cp.Status)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("attempts: got %d want 3", got)
	}
	if len(results) != 1 || results[0].Attempts != 3 {
		t.Fatalf("result attempts: %#v", results)
	}
	if cp.AttemptsByID["a1"] != 3 {
		t.Fatalf("checkpoint attempts: got %d want 3", cp.AttemptsByID["a1"])
	}
	if cp.Error == "" {
		t.Fatal("expected error message recorded on checkpoint")
	}
}

func TestWorkflow_Given_ActivityFailsThenSucceeds_When_Execute_Then_RetryWins(t *testing.T) {
	var attempts int32
	exec := func(_ context.Context, _ scenarioruns.Activity) error {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			return errors.New("transient")
		}
		return nil
	}
	persist := &memPersister{}
	wf := &scenarioruns.Workflow{
		Policy:  scenarioruns.RetryPolicy{MaxAttempts: 3, BackoffMs: 0},
		Persist: persist,
		Sleep:   func(time.Duration) {},
	}
	cp, results, err := wf.Execute(context.Background(), "run1", "scn1", []scenarioruns.Activity{{ID: "a1", Layer: 0}}, exec)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if cp.Status != scenarioruns.RunStatusSucceeded {
		t.Fatalf("status: got %q want succeeded", cp.Status)
	}
	if results[0].Attempts != 2 {
		t.Fatalf("attempts: got %d want 2", results[0].Attempts)
	}
}

func TestWorkflow_Given_DownstreamLayer_When_UpstreamFails_Then_DownstreamSkipped(t *testing.T) {
	var executed []string
	var mu sync.Mutex
	exec := func(_ context.Context, a scenarioruns.Activity) error {
		mu.Lock()
		executed = append(executed, a.ID)
		mu.Unlock()
		if a.ID == "m1" {
			return errors.New("boom")
		}
		return nil
	}
	wf := &scenarioruns.Workflow{
		Policy:  scenarioruns.RetryPolicy{MaxAttempts: 3, BackoffMs: 0},
		Persist: &memPersister{},
		Sleep:   func(time.Duration) {},
	}
	activities := []scenarioruns.Activity{
		{ID: "m1", Layer: 0},
		{ID: "m2", Layer: 1},
		{ID: "a1", Layer: 2},
	}
	cp, _, err := wf.Execute(context.Background(), "run1", "scn1", activities, exec)
	if err == nil {
		t.Fatal("expected failure")
	}
	if cp.Status != scenarioruns.RunStatusFailed {
		t.Fatalf("status: got %q want failed", cp.Status)
	}
	// m1 retried 3 times; m2 / a1 never run.
	if len(executed) != 3 {
		t.Fatalf("executed: got %v want 3 m1 retries", executed)
	}
	for _, id := range executed {
		if id != "m1" {
			t.Fatalf("downstream ran: %v", executed)
		}
	}
}

func TestWorkflow_Given_LongRunningActivity_When_CtxCancel_Then_RunMarkedCanceled(t *testing.T) {
	started := make(chan struct{})
	exec := func(ctx context.Context, _ scenarioruns.Activity) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	wf := &scenarioruns.Workflow{
		Policy:  scenarioruns.RetryPolicy{MaxAttempts: 1},
		Persist: &memPersister{},
		Sleep:   func(time.Duration) {},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	cp, _, err := wf.Execute(ctx, "run1", "scn1", []scenarioruns.Activity{{ID: "long", Layer: 0}}, exec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err: got %v want context.Canceled", err)
	}
	if cp.Status != scenarioruns.RunStatusCanceled {
		t.Fatalf("status: got %q want canceled", cp.Status)
	}
}

func TestWorkflow_Given_PreCanceledCtx_When_Execute_Then_NoActivityRuns(t *testing.T) {
	var calls int32
	exec := func(_ context.Context, _ scenarioruns.Activity) error {
		atomic.AddInt32(&calls, 1)
		return nil
	}
	wf := &scenarioruns.Workflow{
		Persist: &memPersister{},
		Sleep:   func(time.Duration) {},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cp, _, err := wf.Execute(ctx, "run1", "scn1", []scenarioruns.Activity{{ID: "a1", Layer: 0}}, exec)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err: got %v want context.Canceled", err)
	}
	if cp.Status != scenarioruns.RunStatusCanceled {
		t.Fatalf("status: got %q want canceled", cp.Status)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("activity ran despite pre-canceled ctx")
	}
}

func TestWorkflow_Given_PartialResume_When_Execute_Then_SkipsCompleted(t *testing.T) {
	var ran []string
	var mu sync.Mutex
	exec := func(_ context.Context, a scenarioruns.Activity) error {
		mu.Lock()
		ran = append(ran, a.ID)
		mu.Unlock()
		return nil
	}
	wf := &scenarioruns.Workflow{
		Persist: &memPersister{},
		Sleep:   func(time.Duration) {},
	}
	all := []scenarioruns.Activity{
		{ID: "a1", Layer: 0},
		{ID: "a2", Layer: 1},
		{ID: "a3", Layer: 2},
	}
	resumeFrom := scenarioruns.SkipCompleted(all, []string{"a1"})
	cp, _, err := wf.Execute(context.Background(), "run1", "scn1", resumeFrom, exec)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if cp.Status != scenarioruns.RunStatusSucceeded {
		t.Fatalf("status: got %q want succeeded", cp.Status)
	}
	if len(ran) != 2 || ran[0] != "a2" || ran[1] != "a3" {
		t.Fatalf("ran: got %v want [a2 a3]", ran)
	}
}

func TestWorkflow_Given_SuccessfulRun_When_PersistCalled_Then_FinalStatusIsTerminal(t *testing.T) {
	exec := func(_ context.Context, _ scenarioruns.Activity) error { return nil }
	persist := &memPersister{}
	wf := &scenarioruns.Workflow{
		Persist: persist,
		Sleep:   func(time.Duration) {},
	}
	_, _, err := wf.Execute(context.Background(), "run1", "scn1", []scenarioruns.Activity{{ID: "a1", Layer: 0}}, exec)
	if err != nil {
		t.Fatal(err)
	}
	cps := persist.snapshot()
	if len(cps) == 0 {
		t.Fatal("expected at least one checkpoint")
	}
	last := cps[len(cps)-1]
	if last.Status != scenarioruns.RunStatusSucceeded {
		t.Fatalf("final status: got %q want succeeded", last.Status)
	}
	if !scenarioruns.IsTerminal(last.Status) {
		t.Fatalf("final status not terminal: %q", last.Status)
	}
}

func TestWorkflow_Given_NilPersister_When_Execute_Then_RunsWithoutCheckpointing(t *testing.T) {
	exec := func(_ context.Context, _ scenarioruns.Activity) error { return nil }
	wf := &scenarioruns.Workflow{
		Persist: nil,
		Sleep:   func(time.Duration) {},
	}
	cp, _, err := wf.Execute(context.Background(), "run1", "scn1", []scenarioruns.Activity{{ID: "a1", Layer: 0}}, exec)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if cp.Status != scenarioruns.RunStatusSucceeded {
		t.Fatalf("status: got %q want succeeded", cp.Status)
	}
}

func TestWorkflow_Given_NilExecutor_When_Execute_Then_ReturnsError(t *testing.T) {
	wf := &scenarioruns.Workflow{}
	_, _, err := wf.Execute(context.Background(), "run1", "scn1", nil, nil)
	if err == nil {
		t.Fatal("expected error on nil executor")
	}
}

func TestWorkflow_Given_BackoffSetAndRetry_When_Execute_Then_SleepCalledBetweenAttempts(t *testing.T) {
	exec := func(_ context.Context, _ scenarioruns.Activity) error {
		return errors.New("boom")
	}
	var sleepCalls []time.Duration
	wf := &scenarioruns.Workflow{
		Policy:  scenarioruns.RetryPolicy{MaxAttempts: 3, BackoffMs: 50},
		Persist: &memPersister{},
		Sleep: func(d time.Duration) {
			sleepCalls = append(sleepCalls, d)
		},
	}
	_, _, _ = wf.Execute(context.Background(), "run1", "scn1", []scenarioruns.Activity{{ID: "a1", Layer: 0}}, exec)
	// 3 attempts → 2 sleeps between them.
	if len(sleepCalls) != 2 {
		t.Fatalf("sleep calls: got %d want 2", len(sleepCalls))
	}
	for _, d := range sleepCalls {
		if d != 50*time.Millisecond {
			t.Fatalf("backoff: got %v want 50ms", d)
		}
	}
}
