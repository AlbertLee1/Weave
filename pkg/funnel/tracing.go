package funnel

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// OSV2-306 — pkg/funnel injects the W3C TraceContext into NATS message
// headers on publish and extracts it on consume so the HTTP→Action→Funnel
// →Bleve trace shows up as one connected trace in Jaeger / Tempo, not two
// disjoint trees on either side of the NATS edge. Implementation reuses
// the global propagator that pkg/tracing.Init installs at boot
// (TraceContext + Baggage composite). Tests install a per-test propagator
// to stay self-contained.

// natsHeaderCarrier adapts a nats.Header to the TextMapCarrier interface
// the OpenTelemetry propagator expects. nats.Header is itself a
// map[string][]string (an alias of http.Header), so Get/Set/Keys delegate
// directly without copying.
type natsHeaderCarrier nats.Header

// Get returns the first value for the given header name, or "" if absent.
func (c natsHeaderCarrier) Get(key string) string { return nats.Header(c).Get(key) }

// Set replaces any existing values for the given header name with a single
// new value. The propagator only writes one value per key so this matches
// the http.Header contract used by propagation.HeaderCarrier.
func (c natsHeaderCarrier) Set(key, value string) { nats.Header(c).Set(key, value) }

// Keys returns the canonical key names present in the carrier. It is used
// by the propagator's serialisation path to discover which fields to
// inject; the order is not significant.
func (c natsHeaderCarrier) Keys() []string {
	out := make([]string, 0, len(c))
	for k := range c {
		out = append(out, k)
	}
	return out
}

// InjectTraceContext writes the current trace context from ctx into the
// supplied NATS header using the globally-configured TextMapPropagator.
// Safe to call when no span is active — the propagator simply writes
// nothing in that case.
func InjectTraceContext(ctx context.Context, h nats.Header) {
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(h))
}

// ExtractTraceContext returns a context populated with whatever trace
// context the NATS header carries. Malformed or missing headers are
// tolerated: the propagator falls back to the supplied parent ctx so the
// consumer can still start a fresh root span.
func ExtractTraceContext(ctx context.Context, h nats.Header) context.Context {
	if h == nil {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, natsHeaderCarrier(h))
}

// publishSpanName and consumeSpanName keep the span names in one place so
// tests and dashboards can grep / filter on a stable string.
const (
	publishSpanName = "funnel.publish"
	consumeSpanName = "funnel.consume"
)

// publishAttributes is the keys we stamp on the publish span. They are
// chosen so that a single trace tells the operator which subject the edit
// landed on and how big the batch was without having to fetch the payload.
// The corresponding attribute strings are written inline at the call site
// because attribute.KeyValue is cheap to construct.
var _ = propagation.TraceContext{} // keep the import even if tracing is off
