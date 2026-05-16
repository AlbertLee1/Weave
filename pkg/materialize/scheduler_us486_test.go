package materialize

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// US-486: Materialization datasets run on schedule, retry on failure, and
// persist state. The Scheduler drives one or more named MaterializeJobs;
// every job has a Compute func that the scheduler invokes on its
// configured interval. Failures inside Compute are retried with
// exponential backoff up to MaxAttempts; the per-job JobStatus records
// LastRunStart / LastSuccess / LastFailure / ConsecutiveFailures /
// TotalRuns / TotalFailures / LastError so operators can answer "when
// did dataset X last refresh, and is it healthy?".

func TestScheduler_RunOnce_ScheduleTrigger_InvokesComputeAndStampsSuccess(t *testing.T) {
	t.Parallel()
	var calls int32
	s := NewScheduler()
	if err := s.Add(MaterializeJob{
		Name:     "northwind.orders.daily",
		Interval: time.Hour,
		Compute: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.RunOnce(context.Background(), "northwind.orders.daily"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("compute calls = %d, want 1", got)
	}
	st, ok := s.Status("northwind.orders.daily")
	if !ok {
		t.Fatal("Status: missing job")
	}
	if st.LastSuccess.IsZero() {
		t.Fatalf("Status.LastSuccess is zero; want non-zero, got %+v", st)
	}
	if st.TotalRuns != 1 {
		t.Fatalf("TotalRuns = %d, want 1", st.TotalRuns)
	}
	if st.TotalFailures != 0 {
		t.Fatalf("TotalFailures = %d, want 0", st.TotalFailures)
	}
	if st.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d, want 0", st.ConsecutiveFailures)
	}
	if st.LastError != "" {
		t.Fatalf("LastError = %q, want empty", st.LastError)
	}
}

func TestScheduler_RunOnce_RetriesOnFailure_ThenSucceeds(t *testing.T) {
	t.Parallel()
	var attempts int32
	target := int32(3) // succeed on the third attempt
	s := NewScheduler()
	s.SetSleepFunc(func(ctx context.Context, _ time.Duration) error {
		return ctx.Err()
	})
	if err := s.Add(MaterializeJob{
		Name:        "retry.eventually.succeeds",
		Interval:    time.Hour,
		MaxAttempts: 5,
		BaseBackoff: time.Millisecond,
		Compute: func(ctx context.Context) error {
			n := atomic.AddInt32(&attempts, 1)
			if n < target {
				return fmt.Errorf("transient failure attempt %d", n)
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.RunOnce(context.Background(), "retry.eventually.succeeds"); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != target {
		t.Fatalf("compute attempts = %d, want %d", got, target)
	}
	st, _ := s.Status("retry.eventually.succeeds")
	if st.LastSuccess.IsZero() {
		t.Fatalf("LastSuccess is zero after eventual success: %+v", st)
	}
	if st.TotalRuns != 1 {
		t.Fatalf("TotalRuns = %d, want 1 (a single RunOnce counts once regardless of retries)", st.TotalRuns)
	}
	if st.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d after success, want 0", st.ConsecutiveFailures)
	}
	if st.LastError != "" {
		t.Fatalf("LastError = %q after success, want empty", st.LastError)
	}
}

func TestScheduler_RunOnce_ExhaustsAttempts_RecordsFailure(t *testing.T) {
	t.Parallel()
	var attempts int32
	boom := errors.New("compute always fails")
	s := NewScheduler()
	s.SetSleepFunc(func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
	if err := s.Add(MaterializeJob{
		Name:        "retry.exhausted",
		Interval:    time.Hour,
		MaxAttempts: 3,
		BaseBackoff: time.Millisecond,
		Compute: func(ctx context.Context) error {
			atomic.AddInt32(&attempts, 1)
			return boom
		},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	err := s.RunOnce(context.Background(), "retry.exhausted")
	if err == nil {
		t.Fatal("RunOnce returned nil error after exhausting attempts")
	}
	if !errors.Is(err, boom) {
		t.Fatalf("RunOnce err = %v, want wraps %v", err, boom)
	}
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("compute attempts = %d, want 3", got)
	}
	st, _ := s.Status("retry.exhausted")
	if !st.LastFailure.After(st.LastSuccess) {
		t.Fatalf("LastFailure (%v) should be after LastSuccess (%v) after exhaustion", st.LastFailure, st.LastSuccess)
	}
	if st.ConsecutiveFailures != 1 {
		t.Fatalf("ConsecutiveFailures = %d, want 1 (one failed run counts as one consecutive failure)", st.ConsecutiveFailures)
	}
	if st.TotalFailures != 1 {
		t.Fatalf("TotalFailures = %d, want 1", st.TotalFailures)
	}
	if st.LastError == "" {
		t.Fatal("LastError is empty after exhaustion")
	}
}

func TestScheduler_RunOnce_RecoversAfterPastFailure_ResetsConsecutiveCounter(t *testing.T) {
	t.Parallel()
	var attempts int32
	s := NewScheduler()
	s.SetSleepFunc(func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
	if err := s.Add(MaterializeJob{
		Name:        "recover",
		Interval:    time.Hour,
		MaxAttempts: 1, // no internal retries; we drive via RunOnce
		BaseBackoff: time.Millisecond,
		Compute: func(ctx context.Context) error {
			n := atomic.AddInt32(&attempts, 1)
			if n == 1 {
				return errors.New("first run fails")
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	// First run: fails.
	if err := s.RunOnce(context.Background(), "recover"); err == nil {
		t.Fatal("first RunOnce: expected error, got nil")
	}
	st, _ := s.Status("recover")
	if st.ConsecutiveFailures != 1 || st.TotalFailures != 1 || st.TotalRuns != 1 {
		t.Fatalf("after fail: %+v", st)
	}
	// Second run: succeeds. Consecutive failure counter must reset.
	if err := s.RunOnce(context.Background(), "recover"); err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	st, _ = s.Status("recover")
	if st.ConsecutiveFailures != 0 {
		t.Fatalf("ConsecutiveFailures = %d after recovery, want 0", st.ConsecutiveFailures)
	}
	if st.TotalRuns != 2 {
		t.Fatalf("TotalRuns = %d, want 2", st.TotalRuns)
	}
	if st.TotalFailures != 1 {
		t.Fatalf("TotalFailures = %d, want 1 (the historical failure should remain counted)", st.TotalFailures)
	}
}

func TestScheduler_RunOnce_UnknownJob_ReturnsError(t *testing.T) {
	t.Parallel()
	s := NewScheduler()
	err := s.RunOnce(context.Background(), "missing")
	if err == nil {
		t.Fatal("RunOnce with unknown job returned nil error")
	}
}

func TestScheduler_Add_RejectsBlankNameOrNilCompute(t *testing.T) {
	t.Parallel()
	s := NewScheduler()
	if err := s.Add(MaterializeJob{Name: "", Compute: func(context.Context) error { return nil }}); err == nil {
		t.Fatal("Add with blank Name returned nil error")
	}
	if err := s.Add(MaterializeJob{Name: "x", Compute: nil}); err == nil {
		t.Fatal("Add with nil Compute returned nil error")
	}
}

func TestScheduler_Add_RejectsDuplicateName(t *testing.T) {
	t.Parallel()
	s := NewScheduler()
	job := MaterializeJob{Name: "dup", Compute: func(context.Context) error { return nil }, Interval: time.Minute}
	if err := s.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := s.Add(job); err == nil {
		t.Fatal("duplicate Add returned nil error")
	}
}

func TestScheduler_RunLoop_TicksOnInterval_AndStopsOnContextCancel(t *testing.T) {
	t.Parallel()
	var calls int32
	s := NewScheduler()
	if err := s.Add(MaterializeJob{
		Name:     "tick",
		Interval: 10 * time.Millisecond,
		Compute: func(ctx context.Context) error {
			atomic.AddInt32(&calls, 1)
			return nil
		},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RunLoop(ctx, nil)
		close(done)
	}()
	// Wait until at least 2 ticks fire.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&calls) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt32(&calls); got < 2 {
		cancel()
		<-done
		t.Fatalf("calls = %d after 2s; expected >= 2 ticks at 10ms cadence", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunLoop did not stop after context cancellation")
	}
}

func TestScheduler_RunLoop_OnError_ReportsFailedJobName(t *testing.T) {
	t.Parallel()
	boom := errors.New("rebuild failed")
	s := NewScheduler()
	s.SetSleepFunc(func(ctx context.Context, _ time.Duration) error { return ctx.Err() })
	if err := s.Add(MaterializeJob{
		Name:        "broken",
		Interval:    10 * time.Millisecond,
		MaxAttempts: 1,
		Compute:     func(ctx context.Context) error { return boom },
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var (
		mu       sync.Mutex
		seenJob  string
		seenErr  error
		errFired = make(chan struct{}, 1)
	)
	onError := func(jobName string, err error) {
		mu.Lock()
		defer mu.Unlock()
		seenJob = jobName
		seenErr = err
		select {
		case errFired <- struct{}{}:
		default:
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.RunLoop(ctx, onError)
		close(done)
	}()

	select {
	case <-errFired:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("onError was never invoked after a failing job")
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if seenJob != "broken" {
		t.Fatalf("seen job = %q, want %q", seenJob, "broken")
	}
	if !errors.Is(seenErr, boom) {
		t.Fatalf("seen err = %v, want wraps %v", seenErr, boom)
	}
}

func TestScheduler_RunOnce_RespectsContextCancellation(t *testing.T) {
	t.Parallel()
	s := NewScheduler()
	started := make(chan struct{})
	release := make(chan struct{})
	if err := s.Add(MaterializeJob{
		Name:        "slow",
		Interval:    time.Hour,
		MaxAttempts: 1,
		Compute: func(ctx context.Context) error {
			close(started)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.RunOnce(ctx, "slow")
	}()
	<-started
	cancel()
	close(release)
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("RunOnce returned nil after context cancel; want non-nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunOnce did not return after context cancellation")
	}
}

func TestScheduler_Status_ListAll_ReturnsCopy(t *testing.T) {
	t.Parallel()
	s := NewScheduler()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := s.Add(MaterializeJob{
			Name:     name,
			Interval: time.Hour,
			Compute:  func(context.Context) error { return nil },
		}); err != nil {
			t.Fatalf("Add %s: %v", name, err)
		}
	}
	all := s.ListStatus()
	if len(all) != 3 {
		t.Fatalf("ListStatus len = %d, want 3", len(all))
	}
	// Mutate the returned slice; the scheduler's internal state must not change.
	all[0].LastError = "tampered"
	if st, _ := s.Status(all[0].Name); st.LastError == "tampered" {
		t.Fatal("ListStatus returned reference; mutations leaked into the scheduler")
	}
}
