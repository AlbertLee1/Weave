package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// setupDeleteJobRouter mounts every async-job route the DELETE handler may
// interact with so the test exercises the full chi -> handler path. US-426.
func setupDeleteJobRouter(handler *Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/api/v2/ontologies/{ontologyApiName}/actions/jobs/{jobId}", handler.GetJob)
	r.Delete("/api/v2/ontologies/{ontologyApiName}/actions/jobs/{jobId}", handler.DeleteJob)
	return r
}

// TestHandler_DeleteJob_FlipsStatusAndSignalsCancel verifies that DELETE on a
// PENDING/RUNNING job (a) flips the durable row to CANCELED before responding,
// and (b) fires the registered cancel func so the worker observes ctx.Done().
func TestHandler_DeleteJob_FlipsStatusAndSignalsCancel(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	store := newMemActionJobStore()
	exec.SetActionJobStore(store)
	handler := NewHandler(exec)
	router := setupDeleteJobRouter(handler)

	// Seed a RUNNING job + register a cancel func like the real worker would.
	job := &ActionJob{
		JobID:    "job-running-1",
		Status:   ActionJobStatusRunning,
		Progress: 42,
	}
	if err := store.CreateActionJob(context.Background(), job); err != nil {
		t.Fatalf("seed job: %v", err)
	}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	var canceled atomic.Bool
	exec.RegisterJobCancel(job.JobID, func() { canceled.Store(true); cancel() })

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v2/ontologies/ont-1/actions/jobs/job-running-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	var resp ActionJob
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Status != ActionJobStatusCanceled {
		t.Fatalf("response status = %q; want CANCELED", resp.Status)
	}
	if resp.ErrorMessage != "canceled" {
		t.Fatalf("response errorMessage = %q; want \"canceled\"", resp.ErrorMessage)
	}
	if !canceled.Load() {
		t.Fatalf("cancel func was not fired")
	}

	// Durable row must reflect the post-cancel state.
	persisted, err := store.GetActionJob(context.Background(), "job-running-1")
	if err != nil {
		t.Fatalf("GetActionJob: %v", err)
	}
	if persisted.Status != ActionJobStatusCanceled {
		t.Fatalf("persisted status = %q; want CANCELED", persisted.Status)
	}
}

// TestHandler_DeleteJob_NotFound returns 404 for unknown job IDs.
func TestHandler_DeleteJob_NotFound(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	exec.SetActionJobStore(newMemActionJobStore())
	handler := NewHandler(exec)
	router := setupDeleteJobRouter(handler)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v2/ontologies/ont-1/actions/jobs/missing", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandler_DeleteJob_TerminalConflict rejects DELETE on already-terminal
// jobs so callers don't silently get a no-op cancel.
func TestHandler_DeleteJob_TerminalConflict(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	store := newMemActionJobStore()
	exec.SetActionJobStore(store)
	handler := NewHandler(exec)
	router := setupDeleteJobRouter(handler)

	for _, status := range []string{
		ActionJobStatusSucceeded,
		ActionJobStatusFailed,
		ActionJobStatusCanceled,
	} {
		j := &ActionJob{
			JobID:    "done-" + status,
			Status:   status,
			Progress: 100,
		}
		if err := store.CreateActionJob(context.Background(), j); err != nil {
			t.Fatalf("seed %s: %v", status, err)
		}
		req := httptest.NewRequest(http.MethodDelete,
			"/api/v2/ontologies/ont-1/actions/jobs/done-"+status, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusConflict {
			t.Fatalf("status %s: expected 409, got %d", status, w.Code)
		}
	}
}

// TestHandler_DeleteJob_DegradedModeNoStore returns 404 when no store is
// wired so the wire shape is consistent with GetJob in degraded mode.
func TestHandler_DeleteJob_DegradedModeNoStore(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	// Intentionally NOT calling SetActionJobStore.
	handler := NewHandler(exec)
	router := setupDeleteJobRouter(handler)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v2/ontologies/ont-1/actions/jobs/anything", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

// TestHandler_DeleteJob_CancelSignalNoOpStillFlipsRow covers the race where
// the runner already exited (no registered cancel) but the row hasn't been
// flushed yet: DELETE must still flip the durable status to CANCELED rather
// than leaving the row in PENDING/RUNNING permanently.
func TestHandler_DeleteJob_CancelSignalNoOpStillFlipsRow(t *testing.T) {
	exec := NewExecutor(&mockOmsRepo{}, nil)
	store := newMemActionJobStore()
	exec.SetActionJobStore(store)
	handler := NewHandler(exec)
	router := setupDeleteJobRouter(handler)

	job := &ActionJob{
		JobID:    "job-orphan-1",
		Status:   ActionJobStatusRunning,
		Progress: 50,
	}
	if err := store.CreateActionJob(context.Background(), job); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Intentionally do NOT register a cancel func — simulates the worker
	// having finished but the durable status flush race lost.

	req := httptest.NewRequest(http.MethodDelete,
		"/api/v2/ontologies/ont-1/actions/jobs/job-orphan-1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
	persisted, err := store.GetActionJob(context.Background(), "job-orphan-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if persisted.Status != ActionJobStatusCanceled {
		t.Fatalf("status = %q; want CANCELED", persisted.Status)
	}
}

// TestMemActionJobStore_ReapOldActionJobs covers the in-memory store's reaper
// semantics — terminal-state rows older than the cutoff are dropped, others
// are preserved.
func TestMemActionJobStore_ReapOldActionJobs(t *testing.T) {
	store := newMemActionJobStore()
	now := time.Now()
	old := now.Add(-25 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	cases := []struct {
		id     string
		status string
		ts     time.Time
		keep   bool
	}{
		{"old-success", ActionJobStatusSucceeded, old, false},
		{"old-failed", ActionJobStatusFailed, old, false},
		{"old-canceled", ActionJobStatusCanceled, old, false},
		{"old-pending", ActionJobStatusPending, old, true},   // non-terminal preserved
		{"old-running", ActionJobStatusRunning, old, true},   // non-terminal preserved
		{"recent-success", ActionJobStatusSucceeded, recent, true}, // not yet stale
	}
	for _, c := range cases {
		j := &ActionJob{JobID: c.id, Status: c.status}
		if err := store.CreateActionJob(context.Background(), j); err != nil {
			t.Fatalf("seed %s: %v", c.id, err)
		}
		// memActionJobStore.UpdateActionJob refreshes UpdatedAt, so set it
		// directly via the underlying map for deterministic age control.
		store.mu.Lock()
		store.jobs[c.id].UpdatedAt = c.ts
		store.mu.Unlock()
	}

	cutoff := now.Add(-24 * time.Hour)
	n, err := store.ReapOldActionJobs(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("ReapOldActionJobs: %v", err)
	}
	if n != 3 {
		t.Errorf("dropped %d; want 3", n)
	}
	for _, c := range cases {
		_, err := store.GetActionJob(context.Background(), c.id)
		got := err == nil
		if got != c.keep {
			t.Errorf("%s: present=%v err=%v; want present=%v", c.id, got, err, c.keep)
		}
	}
}

// reapingStore wraps memActionJobStore to count ReapOldActionJobs invocations
// so the loop test can assert at-least-one tick fired without flaky sleeps.
type reapingStore struct {
	*memActionJobStore
	calls atomic.Int32
	gate  chan struct{}
}

func (r *reapingStore) ReapOldActionJobs(ctx context.Context, olderThan time.Time) (int64, error) {
	defer r.calls.Add(1)
	defer func() {
		select {
		case r.gate <- struct{}{}:
		default:
		}
	}()
	return r.memActionJobStore.ReapOldActionJobs(ctx, olderThan)
}

// TestRunActionJobReaperLoop_TicksAndStopsOnContext verifies the loop fires at
// least once per tick interval, calls ReapOldActionJobs, and exits cleanly
// when the parent context cancels.
func TestRunActionJobReaperLoop_TicksAndStopsOnContext(t *testing.T) {
	mem := newMemActionJobStore()
	rs := &reapingStore{memActionJobStore: mem, gate: make(chan struct{}, 8)}

	// Seed an old terminal row + a recent one.
	now := time.Now()
	old := &ActionJob{JobID: "old", Status: ActionJobStatusSucceeded, UpdatedAt: now.Add(-25 * time.Hour)}
	recent := &ActionJob{JobID: "recent", Status: ActionJobStatusSucceeded, UpdatedAt: now.Add(-1 * time.Hour)}
	if err := mem.CreateActionJob(context.Background(), old); err != nil {
		t.Fatalf("seed old: %v", err)
	}
	if err := mem.CreateActionJob(context.Background(), recent); err != nil {
		t.Fatalf("seed recent: %v", err)
	}
	mem.jobs["old"].UpdatedAt = old.UpdatedAt
	mem.jobs["recent"].UpdatedAt = recent.UpdatedAt

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	var reapedTotal atomic.Int64
	go func() {
		defer close(done)
		RunActionJobReaperLoop(ctx, rs, 20*time.Millisecond, 24*time.Hour,
			func(n int64) { reapedTotal.Add(n) },
			func(err error) { t.Errorf("unexpected error: %v", err) },
		)
	}()

	// Wait for at least one reap call.
	select {
	case <-rs.gate:
	case <-time.After(time.Second):
		t.Fatal("ReapOldActionJobs was never called")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not exit after context cancel")
	}

	if rs.calls.Load() < 1 {
		t.Errorf("calls = %d; want >= 1", rs.calls.Load())
	}
	if reapedTotal.Load() < 1 {
		t.Errorf("reaped total = %d; want >= 1 (the 25h-old row)", reapedTotal.Load())
	}
	// The recent row must still be present.
	if _, err := mem.GetActionJob(context.Background(), "recent"); err != nil {
		t.Errorf("recent row should still be present: %v", err)
	}
	// The old row must be gone.
	if _, err := mem.GetActionJob(context.Background(), "old"); err == nil {
		t.Errorf("old row should have been reaped")
	}
}

// TestRunActionJobReaperLoop_NilStoreNoOp ensures the loop is safe to call
// with a nil store (degraded-mode boot) without panicking.
func TestRunActionJobReaperLoop_NilStoreNoOp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled
	// Both shapes of "do nothing" — nil store and zero interval — must be no-ops.
	RunActionJobReaperLoop(ctx, nil, time.Hour, 24*time.Hour, nil, nil)
	RunActionJobReaperLoop(ctx, newMemActionJobStore(), 0, 24*time.Hour, nil, nil)
	RunActionJobReaperLoop(ctx, newMemActionJobStore(), time.Hour, 0, nil, nil)
}

// errorReapStore returns a synthetic error from ReapOldActionJobs so the loop
// can be exercised against the onError path.
type errorReapStore struct {
	*memActionJobStore
	gate chan struct{}
}

func (e *errorReapStore) ReapOldActionJobs(_ context.Context, _ time.Time) (int64, error) {
	defer func() {
		select {
		case e.gate <- struct{}{}:
		default:
		}
	}()
	return 0, oms.ErrNotFound // any non-nil error
}

// TestRunActionJobReaperLoop_OnErrorContinuesLoop verifies a transient error
// from ReapOldActionJobs surfaces through onError and the loop keeps running
// (rather than exiting on the first failure).
func TestRunActionJobReaperLoop_OnErrorContinuesLoop(t *testing.T) {
	store := &errorReapStore{
		memActionJobStore: newMemActionJobStore(),
		gate:              make(chan struct{}, 4),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var errCount atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunActionJobReaperLoop(ctx, store, 10*time.Millisecond, 24*time.Hour,
			nil,
			func(_ error) { errCount.Add(1) },
		)
	}()

	// Wait for at least two reap calls so we know the loop survived the
	// first error.
	for i := 0; i < 2; i++ {
		select {
		case <-store.gate:
		case <-time.After(time.Second):
			t.Fatalf("only %d reap calls observed", i)
		}
	}
	cancel()
	<-done
	if errCount.Load() < 2 {
		t.Errorf("errCount = %d; want >= 2", errCount.Load())
	}
}
