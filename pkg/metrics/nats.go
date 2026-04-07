package metrics

import "time"

// statusLabel maps a Go error to the constant "ok" / "error" label used by
// every Weave counter that partitions by success state. Centralising this
// avoids subtle drift between subsystems.
func statusLabel(err error) string {
	if err == nil {
		return "ok"
	}
	return "error"
}

// NATSPublish records a single NATS publish attempt. Call this immediately
// after the publisher returns: pass the subject, the publish error (or nil
// on success), and the elapsed time spent inside the publish call.
func NATSPublish(subject string, err error, duration time.Duration) {
	natsPublishTotal.WithLabelValues(subject, statusLabel(err)).Inc()
	// Publishes are typically too short to be interesting on their own, but
	// we keep the duration parameter so callers have a single instrumentation
	// surface and we can decide later to add a histogram without changing
	// every call site. Currently a no-op for the publish path.
	_ = duration
}

// NATSConsume records a single NATS message consumption. Call this from
// the consumer's per-message handler with the subject, processing error
// (or nil), and the elapsed processing time.
func NATSConsume(subject string, err error, duration time.Duration) {
	natsConsumeTotal.WithLabelValues(subject, statusLabel(err)).Inc()
	observeDuration(natsConsumeDuration.WithLabelValues(subject), duration)
}
