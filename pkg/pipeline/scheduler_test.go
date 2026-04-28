package pipeline

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// schedRecorder captures every pipeline the Scheduler hands to the
// runner so tests can assert on the dispatch shape without standing up
// the real DAG executor. Distinct from runner_test.go's recordingRunner
// (that one wraps NodeRunner; this one wraps PipelineRunner).
type schedRecorder struct {
	mu      sync.Mutex
	runs    []string
	pipes   []*Pipeline
	failOn  map[string]error
	delay   time.Duration
	counter int64
}

func newSchedRecorder() *schedRecorder {
	return &schedRecorder{failOn: map[string]error{}}
}

func (r *schedRecorder) RunPipeline(ctx context.Context, p *Pipeline) error {
	atomic.AddInt64(&r.counter, 1)
	if r.delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(r.delay):
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, p.ID)
	r.pipes = append(r.pipes, ClonePipeline(p))
	if err, ok := r.failOn[p.ID]; ok {
		return err
	}
	return nil
}

func (r *schedRecorder) lastFor(id string) *Pipeline {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := len(r.pipes) - 1; i >= 0; i-- {
		if r.pipes[i].ID == id {
			return r.pipes[i]
		}
	}
	return nil
}

// schedFixture returns a minimal Pipeline that satisfies Validate().
// The schedule and enabled flag are caller-supplied so each test can
// dial them independently. Separate from pipeline_test.go's
// validPipeline (that one returns a fixed pipeline).
func schedFixture(id, schedule string, enabled bool) *Pipeline {
	return &Pipeline{
		ID:       id,
		Name:     id,
		Inputs:   []Input{{Name: "src", Type: "objectset"}},
		Outputs:  []Output{{Name: "sink", Type: "log", Input: "src"}},
		Schedule: schedule,
		Enabled:  enabled,
	}
}

// waitForRuns blocks until r records at least n executions or the
// timeout elapses. Returns the actual run count observed.
func waitForRuns(t *testing.T, r *schedRecorder, n int, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		got := atomic.LoadInt64(&r.counter)
		if int(got) >= n {
			return int(got)
		}
		if time.Now().After(deadline) {
			return int(got)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestScheduler_StartLoadsEnabledScheduledPipelines(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.CreatePipeline(ctx, schedFixture("p1", "@every 1s", true)); err != nil {
		t.Fatalf("create p1: %v", err)
	}
	if err := store.CreatePipeline(ctx, schedFixture("p2", "@every 1s", false)); err != nil {
		t.Fatalf("create p2 (disabled): %v", err)
	}
	if err := store.CreatePipeline(ctx, schedFixture("p3", "", true)); err != nil {
		t.Fatalf("create p3 (no schedule): %v", err)
	}

	sched := NewScheduler(store, newSchedRecorder())
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sched.Stop()

	got := sched.Entries()
	if _, ok := got["p1"]; !ok {
		t.Fatalf("expected p1 to be registered, got entries=%v", got)
	}
	if _, ok := got["p2"]; ok {
		t.Errorf("disabled pipeline p2 must not be registered")
	}
	if _, ok := got["p3"]; ok {
		t.Errorf("pipeline with empty schedule must not be registered")
	}
	if len(got) != 1 {
		t.Errorf("expected exactly 1 entry, got %d (%v)", len(got), got)
	}
}

func TestScheduler_CronTickFiresRunner(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.CreatePipeline(ctx, schedFixture("fast", "@every 100ms", true)); err != nil {
		t.Fatalf("create: %v", err)
	}
	runner := newSchedRecorder()
	sched := NewScheduler(store, runner)

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sched.Stop()

	if got := waitForRuns(t, runner, 1, 3*time.Second); got < 1 {
		t.Fatalf("expected at least 1 run, got %d", got)
	}
	if last := runner.lastFor("fast"); last == nil {
		t.Fatalf("expected runner to be invoked with pipeline 'fast'")
	} else if last.Schedule != "@every 100ms" {
		t.Errorf("snapshot Schedule = %q, want %q", last.Schedule, "@every 100ms")
	}
}

func TestScheduler_RegisterAddsAndReplaces(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	sched := NewScheduler(store, newSchedRecorder())
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sched.Stop()

	p := schedFixture("p1", "@every 1s", true)
	if err := store.CreatePipeline(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := sched.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	first := sched.Entries()["p1"]

	// Re-register with a different schedule should swap the cron entry id.
	p2 := schedFixture("p1", "@every 30s", true)
	if err := sched.Register(p2); err != nil {
		t.Fatalf("Register replace: %v", err)
	}
	second := sched.Entries()["p1"]
	if second == first {
		t.Errorf("Register on existing id must replace the cron entry (id unchanged %d)", first)
	}
}

func TestScheduler_RegisterDisabledOrEmptyScheduleUnregisters(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	sched := NewScheduler(store, newSchedRecorder())
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sched.Stop()

	p := schedFixture("p1", "@every 1s", true)
	if err := sched.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, ok := sched.Entries()["p1"]; !ok {
		t.Fatalf("expected p1 to be registered")
	}

	// Flipping enabled=false should drop the entry.
	disabled := schedFixture("p1", "@every 1s", false)
	if err := sched.Register(disabled); err != nil {
		t.Fatalf("Register disabled: %v", err)
	}
	if _, ok := sched.Entries()["p1"]; ok {
		t.Errorf("disabled re-register must unregister the entry")
	}

	// Re-enable, then clear the schedule.
	if err := sched.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	cleared := schedFixture("p1", "", true)
	if err := sched.Register(cleared); err != nil {
		t.Fatalf("Register cleared schedule: %v", err)
	}
	if _, ok := sched.Entries()["p1"]; ok {
		t.Errorf("empty schedule re-register must unregister the entry")
	}
}

func TestScheduler_RegisterRejectsInvalidCron(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	sched := NewScheduler(store, newSchedRecorder())
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sched.Stop()

	bad := schedFixture("p1", "not-a-cron-expr", true)
	if err := sched.Register(bad); err == nil {
		t.Fatalf("expected error for invalid cron expression")
	}
	if _, ok := sched.Entries()["p1"]; ok {
		t.Errorf("invalid cron must not leave a stale entry behind")
	}
}

func TestScheduler_Unregister(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	sched := NewScheduler(store, newSchedRecorder())
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sched.Stop()

	p := schedFixture("p1", "@every 1s", true)
	if err := sched.Register(p); err != nil {
		t.Fatalf("Register: %v", err)
	}
	sched.Unregister("p1")
	if _, ok := sched.Entries()["p1"]; ok {
		t.Errorf("Unregister did not drop the entry")
	}
	// Unregistering an unknown id must be a no-op (no panic).
	sched.Unregister("does-not-exist")
}

func TestScheduler_ReloadSyncsWithStore(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	sched := NewScheduler(store, newSchedRecorder())
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sched.Stop()

	if err := store.CreatePipeline(ctx, schedFixture("a", "@every 1s", true)); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := store.CreatePipeline(ctx, schedFixture("b", "@every 1s", true)); err != nil {
		t.Fatalf("create b: %v", err)
	}

	if err := sched.Reload(ctx); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if got := sched.Entries(); len(got) != 2 {
		t.Fatalf("after reload expected 2 entries, got %d (%v)", len(got), got)
	}

	// Delete b, disable a → next reload should drop both.
	if err := store.DeletePipeline(ctx, "b"); err != nil {
		t.Fatalf("delete b: %v", err)
	}
	disabled := false
	if err := store.UpdatePipeline(ctx, "a", PipelineUpdate{Enabled: &disabled}); err != nil {
		t.Fatalf("disable a: %v", err)
	}
	if err := sched.Reload(ctx); err != nil {
		t.Fatalf("Reload after mutate: %v", err)
	}
	if got := sched.Entries(); len(got) != 0 {
		t.Errorf("after disable+delete expected 0 entries, got %d (%v)", len(got), got)
	}
}

func TestScheduler_StopIdempotentAndPreStartSafe(t *testing.T) {
	sched := NewScheduler(NewMemoryStore(), newSchedRecorder())
	// Stop before Start must not panic.
	sched.Stop()

	ctx := context.Background()
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Double Start is a no-op.
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	sched.Stop()
	// Double Stop is a no-op.
	sched.Stop()
}

func TestScheduler_RunnerErrorIsLoggedNotFatal(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.CreatePipeline(ctx, schedFixture("flaky", "@every 100ms", true)); err != nil {
		t.Fatalf("create: %v", err)
	}
	runner := newSchedRecorder()
	runner.failOn["flaky"] = errors.New("boom")
	sched := NewScheduler(store, runner)
	var logged int64
	sched.SetLogger(func(format string, _ ...any) {
		atomic.AddInt64(&logged, 1)
	})

	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sched.Stop()

	if got := waitForRuns(t, runner, 2, 3*time.Second); got < 2 {
		t.Fatalf("expected runner to keep firing despite errors, got %d runs", got)
	}
	if atomic.LoadInt64(&logged) == 0 {
		t.Errorf("expected SetLogger callback to be invoked at least once on runner error")
	}
}

func TestScheduler_NilStoreOrRunnerAtConstruct(t *testing.T) {
	if NewScheduler(nil, newSchedRecorder()) == nil {
		t.Fatal("nil store should still construct a scheduler (degraded mode)")
	}
	if NewScheduler(NewMemoryStore(), nil) == nil {
		t.Fatal("nil runner should still construct a scheduler (degraded mode)")
	}
	// Start with nil store should error rather than panic.
	sched := NewScheduler(nil, newSchedRecorder())
	if err := sched.Start(context.Background()); err == nil {
		t.Errorf("expected Start with nil store to error")
	}
	// Start with nil runner should error.
	sched2 := NewScheduler(NewMemoryStore(), nil)
	if err := sched2.Start(context.Background()); err == nil {
		t.Errorf("expected Start with nil runner to error")
	}
}

func TestScheduler_StopWaitsForInflightRun(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	if err := store.CreatePipeline(ctx, schedFixture("slow", "@every 100ms", true)); err != nil {
		t.Fatalf("create: %v", err)
	}
	runner := newSchedRecorder()
	runner.delay = 200 * time.Millisecond
	sched := NewScheduler(store, runner)
	if err := sched.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for at least one in-flight run to start.
	if got := waitForRuns(t, runner, 1, 3*time.Second); got < 1 {
		t.Fatalf("expected runner to start, got %d", got)
	}
	stopStart := time.Now()
	sched.Stop()
	if elapsed := time.Since(stopStart); elapsed > 5*time.Second {
		t.Errorf("Stop took too long: %s", elapsed)
	}
	// Stop must not panic; runs counter should not grow further after a tick window.
	before := atomic.LoadInt64(&runner.counter)
	time.Sleep(300 * time.Millisecond)
	after := atomic.LoadInt64(&runner.counter)
	if after != before {
		t.Errorf("runs continued after Stop: before=%d after=%d", before, after)
	}
}
