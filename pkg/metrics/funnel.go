package metrics

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// US-470 metrics — pending DLQ depth.
//
// weave_funnel_dlq_size is a single-series gauge (no labels) tracking the
// number of dead-lettered messages currently held in the
// OBJECT_EDITS_DLQ NATS stream. A periodic poll loop (RunFunnelDLQSizePollLoop)
// refreshes it; the admin list endpoint also pushes a fresh observation each
// time it runs so dashboards stay in sync without waiting for the next tick.
var funnelDLQSize = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "weave_funnel_dlq_size",
		Help: "Number of pending messages in the funnel DLQ (OBJECT_EDITS_DLQ) JetStream stream.",
	},
)

// SetFunnelDLQSize updates the gauge to the supplied non-negative count.
// Negative values are clamped to zero so a transient sizer failure can't
// drive the panel into negative territory.
func SetFunnelDLQSize(n float64) {
	if n < 0 {
		n = 0
	}
	funnelDLQSize.Set(n)
}

// FunnelDLQSizeGauge exposes the package-private gauge so tests in other
// packages (e.g. the admin handler suite) can assert against its observed
// value via prometheus testutil. Production code should not import it.
func FunnelDLQSizeGauge() prometheus.Gauge {
	return funnelDLQSize
}

// DLQSizer is the minimum interface RunFunnelDLQSizePollLoop expects from
// a DLQ reader. Mirrors funnel.DLQReader.Size without importing pkg/funnel
// (avoids an import cycle between metrics and funnel).
type DLQSizer interface {
	Size(ctx context.Context) (int64, error)
}

// ---------------------------------------------------------------------------
// PRD-V2 §4.6 follow-up to round 3's Gap-O4: the readiness handler exposes
// funnel consumer lag as a soft degraded signal, but oncall needs a
// Prometheus gauge for trend and alerting (e.g. PagerDuty if lag stays
// elevated for >5 min). Same poll-loop shape as funnelDLQSize above so
// operators only have one mental model for funnel observability.
// ---------------------------------------------------------------------------

// funnelLagMessages is a single-series gauge (no labels) that mirrors the
// number of unprocessed messages between the JetStream OBJECT_EDITS
// stream tip and the in-process Consumer.LastOffset. RunFunnelConsumerLagPollLoop
// refreshes it; the /health/ready handler's ProbeFunnel path could also
// push a fresh observation in the future when it computes the same number
// for the degraded check (skipping a redundant StreamInfo RPC).
var funnelLagMessages = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Name: "weave_funnel_lag_messages",
		Help: "Number of unprocessed messages between the JetStream OBJECT_EDITS stream tip and the in-process Consumer.LastOffset. PRD-V2 §4.6 Gap-O4.",
	},
)

// SetFunnelConsumerLag updates the gauge to the supplied non-negative
// count. Negative inputs are clamped to zero (defensive — Lag() returns
// uint64 so the float cast can't actually be negative today, but the
// guard keeps the wire shape honest if a future caller subtracts before
// passing in).
func SetFunnelConsumerLag(n float64) {
	if n < 0 {
		n = 0
	}
	funnelLagMessages.Set(n)
}

// FunnelConsumerLagGauge exposes the package-private gauge so other
// packages (e.g. admin handlers, health probes) can assert against its
// observed value or push fresh observations without waiting for the
// next poll tick. Production code should NOT mutate it directly —
// always go through SetFunnelConsumerLag.
func FunnelConsumerLagGauge() prometheus.Gauge {
	return funnelLagMessages
}

// LagSizer is the minimum interface RunFunnelConsumerLagPollLoop expects
// from a Consumer. Mirrors funnel.Consumer.Lag without importing
// pkg/funnel (avoids the same import cycle the DLQSizer interface
// dodges). The intended production wiring passes the funnel.Consumer's
// Lag method directly via a small adapter in cmd/server.
type LagSizer interface {
	Lag() (uint64, error)
}

// RunFunnelConsumerLagPollLoop polls `sizer.Lag()` every `interval` and
// pushes the result onto the funnel lag gauge. The loop exits when ctx
// is cancelled. Nil sizer or non-positive interval makes the loop a
// no-op so degraded-mode boot can wire the goroutine unconditionally.
// `onError`, if supplied, is invoked once per sizer failure; the loop
// continues on error AND the gauge is NOT clobbered — a transient
// StreamInfo blip must not make the lag panel drop to zero on the
// operator's dashboard. The last good observation stays visible until
// the next successful read.
func RunFunnelConsumerLagPollLoop(ctx context.Context, sizer LagSizer, interval time.Duration, onError func(error)) {
	if sizer == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := sizer.Lag()
			if err != nil {
				if onError != nil {
					onError(err)
				}
				continue
			}
			SetFunnelConsumerLag(float64(n))
		}
	}
}

// RunFunnelDLQSizePollLoop polls `sizer.Size(ctx)` every `interval` and
// pushes the result onto the funnel DLQ gauge. The loop exits when ctx is
// cancelled. Nil sizer or non-positive interval makes the loop a no-op so
// degraded-mode boot can wire the goroutine unconditionally. `onError`, if
// supplied, is invoked once per sizer failure; the loop continues on error.
func RunFunnelDLQSizePollLoop(ctx context.Context, sizer DLQSizer, interval time.Duration, onError func(error)) {
	if sizer == nil || interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := sizer.Size(ctx)
			if err != nil {
				if onError != nil {
					onError(err)
				}
				continue
			}
			SetFunnelDLQSize(float64(n))
		}
	}
}
