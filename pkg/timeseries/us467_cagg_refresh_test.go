package timeseries_test

// US-467 unit coverage for the cagg refresh loop driver. Mirrors the
// US-462 SavedSetReaper pattern: the free function `RunCAGGRefreshLoop`
// drives an injected `CAGGRefresher`, so we can verify ticker semantics,
// graceful shutdown, and error tolerance without booting PostgreSQL.

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/timeseries"
)

type fakeCAGGRefresher struct {
	calls atomic.Int64
	errs  atomic.Int64
	// failEvery: when N > 0, return an error on every Nth call (1-indexed).
	failEvery int64
}

func (f *fakeCAGGRefresher) RefreshCAGG(_ context.Context) error {
	n := f.calls.Add(1)
	if f.failEvery > 0 && n%f.failEvery == 0 {
		f.errs.Add(1)
		return errors.New("synthetic refresh failure")
	}
	return nil
}

func TestRunCAGGRefreshLoop_Given_ShortInterval_When_RunForFewTicks_Then_RefreshesAndStopsOnContext(t *testing.T) {
	t.Parallel()
	refresher := &fakeCAGGRefresher{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var refreshCount atomic.Int64
	done := make(chan struct{})
	go func() {
		timeseries.RunCAGGRefreshLoop(ctx, refresher, 10*time.Millisecond,
			func() { refreshCount.Add(1) },
			func(err error) { t.Errorf("unexpected onError: %v", err) })
		close(done)
	}()

	// Wait until refresher has been hit at least 3 times.
	deadline := time.Now().Add(2 * time.Second)
	for refresher.calls.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("refresher only called %d times in 2s", refresher.calls.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh loop did not exit within 1s after cancel")
	}
	if refresher.calls.Load() < 3 {
		t.Errorf("calls=%d, want >=3", refresher.calls.Load())
	}
	if refreshCount.Load() < 3 {
		t.Errorf("onRefresh count=%d, want >=3", refreshCount.Load())
	}
}

func TestRunCAGGRefreshLoop_Given_NilRefresherOrZeroInterval_When_Run_Then_ReturnsImmediately(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// nil refresher
	t0 := time.Now()
	timeseries.RunCAGGRefreshLoop(ctx, nil, time.Second, nil, nil)
	if d := time.Since(t0); d > 50*time.Millisecond {
		t.Errorf("nil refresher took %v, want immediate return", d)
	}

	// zero interval
	t0 = time.Now()
	timeseries.RunCAGGRefreshLoop(ctx, &fakeCAGGRefresher{}, 0, nil, nil)
	if d := time.Since(t0); d > 50*time.Millisecond {
		t.Errorf("zero interval took %v, want immediate return", d)
	}

	// negative interval
	t0 = time.Now()
	timeseries.RunCAGGRefreshLoop(ctx, &fakeCAGGRefresher{}, -time.Second, nil, nil)
	if d := time.Since(t0); d > 50*time.Millisecond {
		t.Errorf("negative interval took %v, want immediate return", d)
	}
}

func TestRunCAGGRefreshLoop_Given_RefresherErrors_When_LoopRunning_Then_LoopContinuesAndCallsOnError(t *testing.T) {
	t.Parallel()
	refresher := &fakeCAGGRefresher{failEvery: 2}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var errCount atomic.Int64
	done := make(chan struct{})
	go func() {
		timeseries.RunCAGGRefreshLoop(ctx, refresher, 5*time.Millisecond, nil,
			func(err error) {
				if err == nil {
					t.Errorf("onError called with nil err")
				}
				errCount.Add(1)
			})
		close(done)
	}()

	// Run until at least one success AND one error are observed.
	deadline := time.Now().Add(2 * time.Second)
	for refresher.calls.Load() < 4 || refresher.errs.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatalf("not enough samples: calls=%d errs=%d", refresher.calls.Load(), refresher.errs.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh loop did not exit within 1s after cancel")
	}

	if errCount.Load() < 1 {
		t.Errorf("onError count=%d, want >=1", errCount.Load())
	}
	if refresher.calls.Load() < 4 {
		t.Errorf("refresher only called %d times (want >=4), loop may have died on first error", refresher.calls.Load())
	}
}

func TestPGStore_SatisfiesCAGGRefresher(t *testing.T) {
	// Compile-time check: *PGStore must implement CAGGRefresher so
	// cmd/server can pass it directly to RunCAGGRefreshLoop.
	var _ timeseries.CAGGRefresher = (*timeseries.PGStore)(nil)
}
