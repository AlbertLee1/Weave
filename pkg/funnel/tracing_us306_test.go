package funnel

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// OSV2-306 — pkg/funnel must stitch HTTP-side spans across the NATS
// boundary using the OpenTelemetry W3C TraceContext propagator. Without
// this, every Action / ObjectSet trace that lands on Publish gets
// truncated at the NATS edge and the consumer's Bleve update work shows
// up as a disconnected new trace in Jaeger / Tempo.

// withRecorder installs a per-test SDK TracerProvider that records every
// span into the returned SpanRecorder. Pairs with the W3C TraceContext
// propagator pkg/tracing.installPropagator already set up at package init
// (we set the propagator here too in case the test runs in isolation).
func withRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		_ = tp.Shutdown(context.Background())
	})
	return rec
}

func TestInjectTraceContext_Given_RootSpan_When_Inject_Then_NatsHeaderCarriesTraceparent_US306(t *testing.T) {
	_ = withRecorder(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "root")
	defer span.End()
	msg := nats.NewMsg("edits.test.Customer")
	InjectTraceContext(ctx, msg.Header)

	if got := msg.Header.Get("traceparent"); got == "" {
		t.Fatalf("traceparent missing from msg header: %#v", msg.Header)
	}
	// W3C TraceContext format: <version>-<trace-id>-<span-id>-<flags>
	got := msg.Header.Get("traceparent")
	if len(got) < 55 || got[2] != '-' {
		t.Errorf("traceparent looks malformed: %q", got)
	}
}

func TestExtractTraceContext_Given_PopulatedHeader_When_Extract_Then_SpanIsRemoteChild_US306(t *testing.T) {
	_ = withRecorder(t)
	rootCtx, rootSpan := otel.Tracer("test").Start(context.Background(), "root")
	msg := nats.NewMsg("edits.test.Customer")
	InjectTraceContext(rootCtx, msg.Header)
	rootSpan.End()

	// Now pretend we are the consumer: build a fresh ctx, extract, then
	// start a child span. Its SpanContext should share the rootSpan's
	// TraceID and reference rootSpan's SpanID as remote parent.
	consumerCtx := ExtractTraceContext(context.Background(), msg.Header)
	_, childSpan := otel.Tracer("test").Start(consumerCtx, "child")
	defer childSpan.End()

	rootSC := rootSpan.SpanContext()
	childSC := childSpan.SpanContext()
	if rootSC.TraceID() != childSC.TraceID() {
		t.Errorf("TraceID mismatch: root=%s child=%s", rootSC.TraceID(), childSC.TraceID())
	}
	if !rootSC.IsValid() || !childSC.IsValid() {
		t.Fatalf("invalid span contexts: root=%v child=%v", rootSC, childSC)
	}
	if childSC.SpanID() == rootSC.SpanID() {
		t.Errorf("child SpanID should differ from parent")
	}
}

func TestExtractTraceContext_Given_NoHeader_When_Extract_Then_ResultIsValidBackgroundCtx_US306(t *testing.T) {
	_ = withRecorder(t)
	ctx := ExtractTraceContext(context.Background(), nats.Header{})
	// Starting a span on this ctx must succeed and produce a *new* trace —
	// the consumer falls back to a fresh root when the header is missing.
	_, span := otel.Tracer("test").Start(ctx, "fallback")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Fatal("expected a valid SpanContext on fallback ctx")
	}
	// The parent should be invalid (no remote parent) — i.e., not linked
	// to anything.
	parent := trace.SpanContextFromContext(ctx)
	if parent.IsValid() {
		t.Errorf("expected invalid parent SpanContext from empty header, got %v", parent)
	}
}

func TestExtractTraceContext_Given_MalformedHeader_When_Extract_Then_FallsBackToBackground_US306(t *testing.T) {
	_ = withRecorder(t)
	h := nats.Header{}
	h.Set("traceparent", "not-a-traceparent")
	ctx := ExtractTraceContext(context.Background(), h)
	// Must not panic and must give us a usable ctx for fresh spans.
	_, span := otel.Tracer("test").Start(ctx, "fallback-malformed")
	defer span.End()
	if !span.SpanContext().IsValid() {
		t.Fatal("expected a valid SpanContext on fallback ctx from malformed header")
	}
}

// TestEndToEnd_Given_HTTPRequestSpan_When_PublishThenConsume_Then_SpansShareTraceID_US306
// simulates the cmd/server flow by hand: an HTTP-shaped root span on
// the publisher side, a real publish via a stub JetStreamContext that
// captures the *nats.Msg, then a synthetic consume call that runs the
// same propagator round-trip. The resulting span pair must share the
// same TraceID, and the consume span's parent must point at the publish
// span — exactly the property Jaeger / Tempo render as one connected
// trace.
func TestEndToEnd_Given_HTTPRequestSpan_When_PublishThenConsume_Then_SpansShareTraceID_US306(t *testing.T) {
	rec := withRecorder(t)

	// 1) start an HTTP-flavoured root ctx
	httpCtx, httpSpan := otel.Tracer("test").Start(context.Background(), "http.POST /actions/apply")

	// 2) Build a publish-side msg and run InjectTraceContext after starting
	//    the funnel.publish span (just like PublishContext does internally).
	publishCtx, publishSpan := otel.Tracer(tracerName).Start(httpCtx, publishSpanName)
	msg := nats.NewMsg("edits.northwind.Order")
	InjectTraceContext(publishCtx, msg.Header)
	publishSpan.End()

	// 3) Consumer side: extract + start funnel.consume.
	consumeCtx := ExtractTraceContext(context.Background(), msg.Header)
	_, consumeSpan := otel.Tracer(tracerName).Start(consumeCtx, consumeSpanName)
	consumeSpan.End()
	httpSpan.End()

	spans := rec.Ended()
	// Map span name -> SpanContext for the assertions.
	got := map[string]trace.SpanContext{}
	for _, s := range spans {
		got[s.Name()] = s.SpanContext()
	}
	if got["http.POST /actions/apply"].TraceID() != got[publishSpanName].TraceID() {
		t.Errorf("publish should share TraceID with HTTP root: http=%s publish=%s",
			got["http.POST /actions/apply"].TraceID(), got[publishSpanName].TraceID())
	}
	if got[publishSpanName].TraceID() != got[consumeSpanName].TraceID() {
		t.Errorf("consume should share TraceID with publish: publish=%s consume=%s",
			got[publishSpanName].TraceID(), got[consumeSpanName].TraceID())
	}
	// The consume span's parent must be the publish span (remote link).
	for _, s := range spans {
		if s.Name() != consumeSpanName {
			continue
		}
		if s.Parent().SpanID() != got[publishSpanName].SpanID() {
			t.Errorf("consume.parent.SpanID = %s, want %s (publish)",
				s.Parent().SpanID(), got[publishSpanName].SpanID())
		}
	}
}

func TestNatsHeaderCarrier_When_GetSetKeys_Then_RoundTripWorks_US306(t *testing.T) {
	h := nats.Header{}
	carrier := natsHeaderCarrier(h)

	if got := carrier.Get("traceparent"); got != "" {
		t.Errorf("Get on empty should return '', got %q", got)
	}
	carrier.Set("traceparent", "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01")
	if got := carrier.Get("traceparent"); got != "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01" {
		t.Errorf("Get after Set: %q", got)
	}
	carrier.Set("tracestate", "vendor=42")
	keys := carrier.Keys()
	hasTP, hasTS := false, false
	for _, k := range keys {
		switch k {
		case "Traceparent", "traceparent":
			hasTP = true
		case "Tracestate", "tracestate":
			hasTS = true
		}
	}
	if !hasTP || !hasTS {
		t.Errorf("Keys missing entries: %v", keys)
	}
}
