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
