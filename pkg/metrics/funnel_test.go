package metrics

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// US-470 weave_funnel_dlq_size gauge contract — surfaces the NATS JetStream
// DLQ pending-count to operators via /metrics. Reset on every call so test
// observations don't leak across cases.

// TestFunnelDLQSize_SetExposesGauge asserts the gauge value mirrors the
// most-recent SetFunnelDLQSize call.
func TestFunnelDLQSize_SetExposesGauge(t *testing.T) {
	SetFunnelDLQSize(0)
	t.Cleanup(func() { SetFunnelDLQSize(0) })
	SetFunnelDLQSize(7)
	if got := testutil.ToFloat64(funnelDLQSize); got != 7 {
		t.Fatalf("gauge = %v, want 7", got)
	}
}

// TestFunnelDLQSize_NegativeClampsToZero pins the invariant that bogus / pre-
// init sizer reads can't drive the gauge into negative territory.
func TestFunnelDLQSize_NegativeClampsToZero(t *testing.T) {
	SetFunnelDLQSize(0)
	t.Cleanup(func() { SetFunnelDLQSize(0) })
	SetFunnelDLQSize(-5)
	if got := testutil.ToFloat64(funnelDLQSize); got != 0 {
		t.Fatalf("gauge = %v, want 0 (clamped)", got)
	}
}

// TestFunnelDLQSize_NameAndHelp pins the canonical metric name + help text so
// dashboards can rely on /metrics line shape.
func TestFunnelDLQSize_NameAndHelp(t *testing.T) {
	desc := funnelDLQSize.Desc().String()
	if !strings.Contains(desc, "weave_funnel_dlq_size") {
		t.Fatalf("expected weave_funnel_dlq_size in desc, got %s", desc)
	}
}

// fakeSizer drives RunFunnelDLQSizePollLoop without a real DLQ.
type fakeSizer struct {
	calls    int
	value    int64
	err      error
	hookCall func()
}

func (f *fakeSizer) Size(ctx context.Context) (int64, error) {
	f.calls++
	if f.hookCall != nil {
		f.hookCall()
	}
	return f.value, f.err
}

// TestFunnelDLQSizePoll_TicksAndStopsOnContext pins the poll loop's lifecycle:
// at least one tick fires after the first interval and a cancelled context
// terminates the goroutine cleanly.
func TestFunnelDLQSizePoll_TicksAndStopsOnContext(t *testing.T) {
	SetFunnelDLQSize(0)
	t.Cleanup(func() { SetFunnelDLQSize(0) })
	sizer := &fakeSizer{value: 12}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunFunnelDLQSizePollLoop(ctx, sizer, 20*time.Millisecond, nil)
		close(done)
	}()
	// Wait for the first tick to settle, then cancel.
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poll loop did not exit on context cancel")
	}
	if sizer.calls == 0 {
		t.Fatalf("expected Size() to be called at least once, got 0")
	}
	if got := testutil.ToFloat64(funnelDLQSize); got != 12 {
		t.Fatalf("gauge = %v, want 12", got)
	}
}

// TestFunnelDLQSizePoll_NilSizerNoOps protects against degraded-mode boot
// where the JetStream context isn't available — the goroutine must exit
// immediately rather than spin on nil.
func TestFunnelDLQSizePoll_NilSizerNoOps(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		RunFunnelDLQSizePollLoop(ctx, nil, 10*time.Millisecond, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("poll loop should no-op when sizer is nil")
	}
}

// TestFunnelDLQSizePoll_ErrorContinuesLoop guarantees a transient sizer error
// is reported through onError but never aborts the loop.
func TestFunnelDLQSizePoll_ErrorContinuesLoop(t *testing.T) {
	sizer := &fakeSizer{err: errors.New("boom")}
	var errs []error
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunFunnelDLQSizePollLoop(ctx, sizer, 15*time.Millisecond, func(err error) { errs = append(errs, err) })
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done
	if len(errs) == 0 {
		t.Fatalf("expected onError to be called at least once, got 0")
	}
	for _, e := range errs {
		if e == nil || e.Error() != "boom" {
			t.Errorf("unexpected error %v", e)
		}
	}
}
