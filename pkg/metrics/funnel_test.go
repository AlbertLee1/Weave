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

// ---------------------------------------------------------------------------
// weave_funnel_lag_messages — PRD-V2 §4.6 Gap-O1 follow-up to round 3's Gap-O4.
// The /health/ready handler already surfaces lag as a degraded signal; this
// gauge is the Prometheus metric oncall scrapes for trend / alerting. Same
// poll-loop shape as the DLQ size gauge above so operators have one mental
// model for both funnel observability surfaces.
// ---------------------------------------------------------------------------

// fakeLagSizer drives RunFunnelConsumerLagPollLoop without a real Consumer.
type fakeLagSizer struct {
	calls int
	value uint64
	err   error
}

func (f *fakeLagSizer) Lag() (uint64, error) {
	f.calls++
	return f.value, f.err
}

// TestBDD_FunnelConsumerLag_GaugeContract pins the wire contract for the
// new weave_funnel_lag_messages gauge: a Set call mirrors verbatim onto the
// gauge, the metric name + help survive into the /metrics page, and the
// gauge is exposed to other packages via FunnelConsumerLagGauge so admin
// handlers can push fresh observations without waiting for the next poll
// tick.
func TestBDD_FunnelConsumerLag_GaugeContract(t *testing.T) {
	t.Run("Set mirrors onto the gauge", func(t *testing.T) {
		SetFunnelConsumerLag(0)
		t.Cleanup(func() { SetFunnelConsumerLag(0) })
		SetFunnelConsumerLag(2417)
		if got := testutil.ToFloat64(funnelLagMessages); got != 2417 {
			t.Fatalf("gauge = %v, want 2417", got)
		}
	})

	t.Run("name and help line shape stays stable for dashboards", func(t *testing.T) {
		desc := funnelLagMessages.Desc().String()
		if !strings.Contains(desc, "weave_funnel_lag_messages") {
			t.Fatalf("expected weave_funnel_lag_messages in desc, got %s", desc)
		}
	})

	t.Run("FunnelConsumerLagGauge exposes the package-private gauge", func(t *testing.T) {
		if g := FunnelConsumerLagGauge(); g == nil {
			t.Fatal("FunnelConsumerLagGauge() must not return nil")
		}
	})
}

// TestBDD_FunnelConsumerLagPoll covers the poll loop's lifecycle (mirrors the
// DLQ size loop): tick once after the first interval, gauge reflects the
// returned value, cancel exits cleanly. Nil sizer no-ops so a degraded-mode
// boot (NATS not wired) doesn't spin a goroutine that wakes every interval
// to do nothing. Errors do NOT clobber the gauge — the last good observation
// stays visible so a transient StreamInfo blip doesn't disappear the lag
// reading.
func TestBDD_FunnelConsumerLagPoll(t *testing.T) {
	t.Run("Tick observes and updates gauge, cancel exits", func(t *testing.T) {
		SetFunnelConsumerLag(0)
		t.Cleanup(func() { SetFunnelConsumerLag(0) })
		sizer := &fakeLagSizer{value: 87}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			RunFunnelConsumerLagPollLoop(ctx, sizer, 20*time.Millisecond, nil)
			close(done)
		}()
		time.Sleep(80 * time.Millisecond)
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("lag poll loop did not exit on context cancel")
		}
		if sizer.calls == 0 {
			t.Fatal("expected Lag() to be called at least once, got 0")
		}
		if got := testutil.ToFloat64(funnelLagMessages); got != 87 {
			t.Fatalf("gauge = %v, want 87", got)
		}
	})

	t.Run("Nil sizer no-ops so degraded boot doesn't spin a wake-every-tick goroutine", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan struct{})
		go func() {
			RunFunnelConsumerLagPollLoop(ctx, nil, 10*time.Millisecond, nil)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(200 * time.Millisecond):
			t.Fatal("lag poll loop should no-op when sizer is nil")
		}
	})

	t.Run("Sizer error fires onError but keeps last good observation visible", func(t *testing.T) {
		SetFunnelConsumerLag(0)
		t.Cleanup(func() { SetFunnelConsumerLag(0) })
		// Seed a healthy value, then flip the sizer to error.
		SetFunnelConsumerLag(42)
		sizer := &fakeLagSizer{err: errors.New("stream info: connection reset")}
		var errs []error
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			RunFunnelConsumerLagPollLoop(ctx, sizer, 15*time.Millisecond, func(err error) { errs = append(errs, err) })
			close(done)
		}()
		time.Sleep(60 * time.Millisecond)
		cancel()
		<-done
		if len(errs) == 0 {
			t.Fatal("expected onError to be called at least once on sizer error")
		}
		// Gauge must NOT have been clobbered by the error path — a
		// transient StreamInfo blip shouldn't make the lag panel
		// disappear / drop to zero on the dashboard. The seeded 42
		// stays visible until the next successful read.
		if got := testutil.ToFloat64(funnelLagMessages); got != 42 {
			t.Fatalf("gauge = %v, want 42 (last good value preserved through errors)", got)
		}
	})
}
