package objectset_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakeReaper records every ReapExpired call so the loop driver can be
// exercised without a real Postgres pool.
type fakeReaper struct {
	calls atomic.Int32
	gate  chan struct{}
	// reapReturn lets a test pin the return value of every call. When non-zero
	// the reaper returns (reapReturn, nil); otherwise it returns (0, reapErr).
	reapReturn int64
	reapErr    error
}

func (f *fakeReaper) ReapExpired(_ context.Context, _ time.Duration) (int64, error) {
	f.calls.Add(1)
	select {
	case f.gate <- struct{}{}:
	default:
	}
	if f.reapErr != nil {
		return 0, f.reapErr
	}
	return f.reapReturn, nil
}

// TestRunSavedSetReaperLoop_TicksAndStopsOnContext verifies the loop fires at
// least once per interval, invokes onReap with the count, and exits cleanly
// when the parent context cancels — US-462 acceptance for the boot ticker.
func TestRunSavedSetReaperLoop_TicksAndStopsOnContext(t *testing.T) {
	fr := &fakeReaper{gate: make(chan struct{}, 8), reapReturn: 1}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var reapedTotal atomic.Int64
	done := make(chan struct{})
	go func() {
		defer close(done)
		objectset.RunSavedSetReaperLoop(ctx, fr, 20*time.Millisecond, time.Hour,
			func(n int64) { reapedTotal.Add(n) },
			func(err error) { t.Errorf("unexpected error: %v", err) },
		)
	}()

	select {
	case <-fr.gate:
	case <-time.After(time.Second):
		t.Fatal("ReapExpired was never called")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not exit after context cancel")
	}

	if fr.calls.Load() < 1 {
		t.Errorf("calls = %d; want >= 1", fr.calls.Load())
	}
	if reapedTotal.Load() < 1 {
		t.Errorf("reaped total = %d; want >= 1", reapedTotal.Load())
	}
}

// TestRunSavedSetReaperLoop_NilOrInvalidNoOp covers the degraded-mode boot
// paths where the loop must not panic: nil reaper (no PG pool) and zero
// interval / ttl.
func TestRunSavedSetReaperLoop_NilOrInvalidNoOp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	objectset.RunSavedSetReaperLoop(ctx, nil, time.Minute, time.Hour, nil, nil)
	objectset.RunSavedSetReaperLoop(ctx, &fakeReaper{gate: make(chan struct{}, 1)}, 0, time.Hour, nil, nil)
	objectset.RunSavedSetReaperLoop(ctx, &fakeReaper{gate: make(chan struct{}, 1)}, time.Minute, 0, nil, nil)
}

// TestRunSavedSetReaperLoop_OnErrorContinues verifies a transient PG error
// surfaces through onError and the loop keeps running rather than exiting on
// the first failure.
func TestRunSavedSetReaperLoop_OnErrorContinues(t *testing.T) {
	fr := &fakeReaper{gate: make(chan struct{}, 8), reapErr: errors.New("synthetic")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var errCount atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		objectset.RunSavedSetReaperLoop(ctx, fr, 10*time.Millisecond, time.Hour,
			nil,
			func(_ error) { errCount.Add(1) },
		)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-fr.gate:
		case <-time.After(time.Second):
			t.Fatalf("ReapExpired tick #%d never fired", i+1)
		}
	}

	cancel()
	<-done

	if errCount.Load() < 2 {
		t.Errorf("onError fired %d times; want >= 2 (loop should not stop on error)", errCount.Load())
	}
}

// TestRunSavedSetReaperLoop_PGSavedStoreSatisfiesInterface guards against a
// future refactor that breaks the boot wire-up by quietly removing
// ReapExpired from *PGSavedStore. The boot in cmd/server/main.go relies on
// the concrete store satisfying the SavedSetReaper interface implicitly.
func TestRunSavedSetReaperLoop_PGSavedStoreSatisfiesInterface(t *testing.T) {
	var _ objectset.SavedSetReaper = (*objectset.PGSavedStore)(nil)
}
