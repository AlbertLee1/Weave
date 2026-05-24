package actions

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Type/value aliases so the SpanKind assertion below stays readable
// without re-importing oteltrace at every call site.
type otelTraceSpanKind = oteltrace.SpanKind

const otelTraceSpanKindClient = oteltrace.SpanKindClient

// TestBDD_HTTPDispatcher_TraceContextPropagation covers PRD-V2
// Observability OpenTelemetry — trace propagation across the
// Weave-action → function-server HTTP edge. Before round 52 the
// HTTPDispatcher built the outbound POST with the request context
// but never called the global propagator's Inject(), so any
// downstream function server saw an empty `traceparent` header and
// had to start a brand-new root span — the trace chain visually
// "snapped" at the dispatcher boundary in Jaeger / Tempo. PRD line
// 126 flagged this as the residual OpenTelemetry gap.
//
// Scenarios:
//   - Inject scenario: when a parent span is active the outbound
//     request must carry a `traceparent` header that references the
//     same TraceID as the parent. Function-server sees one connected
//     trace, not two roots.
//   - Span attributes scenario: the dispatch must produce a client-
//     kind span with http.method / http.url / http.status_code /
//     function.rid attributes so dashboards can group by either
//     dimension.
//   - Error status scenario: when the function server returns 5xx
//     the dispatcher span must be flipped to status Error so head-
//     sampled traces keep the failing path even at fractional sample
//     rate.
//   - No-parent scenario: a request with no incoming TraceContext
//     must still get a fresh outbound traceparent (the dispatch
//     itself becomes the root span and propagates downstream).

func TestBDD_HTTPDispatcher_InjectsTraceContextOnOutboundCall(t *testing.T) {
	// Install a fresh tracer provider + W3C propagator so the test is
	// self-contained even though the production global also installs
	// one at Init() time.
	original := otel.GetTracerProvider()
	originalProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(original)
		otel.SetTextMapPropagator(originalProp)
	})
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var capturedTraceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedTraceparent = r.Header.Get("traceparent")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"edits":[]}`))
	}))
	t.Cleanup(srv.Close)

	d := NewHTTPDispatcher(srv.URL)
	// Start a parent span so the propagator has something to inject.
	tracer := tp.Tracer("test")
	ctx, parent := tracer.Start(context.Background(), "parent.test")
	defer parent.End()

	_, err := d.Dispatch(ctx, &oms.ActionType{
		APIName:     "createOrder",
		RID:         "ri.action.main.createOrder",
		FunctionRID: "fn.createOrder",
	}, map[string]interface{}{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	if capturedTraceparent == "" {
		t.Fatal("server received no traceparent header — propagator was not invoked on outbound request")
	}
	// traceparent format: 00-<trace-id-32hex>-<span-id-16hex>-<flags>
	parts := strings.Split(capturedTraceparent, "-")
	if len(parts) != 4 || len(parts[1]) != 32 {
		t.Fatalf("malformed traceparent %q", capturedTraceparent)
	}
	parentTraceHex := parent.SpanContext().TraceID().String()
	if parts[1] != parentTraceHex {
		t.Errorf("outbound traceparent trace-id=%s does not match parent span trace-id=%s — function server will see a disjoint trace tree",
			parts[1], parentTraceHex)
	}
}

func TestBDD_HTTPDispatcher_EmitsClientSpanWithAttributes(t *testing.T) {
	original := otel.GetTracerProvider()
	originalProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(original)
		otel.SetTextMapPropagator(originalProp)
	})
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"edits":[]}`))
	}))
	t.Cleanup(srv.Close)

	d := NewHTTPDispatcher(srv.URL)
	_, err := d.Dispatch(context.Background(), &oms.ActionType{
		APIName:     "createOrder",
		RID:         "ri.action.main.createOrder",
		FunctionRID: "fn.createOrder",
	}, map[string]interface{}{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	spans := recorder.Ended()
	// Find the dispatch span among any ancillary spans the propagator
	// may emit; the name prefix "function.dispatch" is the contract.
	var dispatchSpan trace.ReadOnlySpan
	for _, s := range spans {
		if strings.HasPrefix(s.Name(), "function.dispatch") {
			dispatchSpan = s
			break
		}
	}
	if dispatchSpan == nil {
		var names []string
		for _, s := range spans {
			names = append(names, s.Name())
		}
		t.Fatalf("no function.dispatch.* span found among %v", names)
	}
	wantName := "function.dispatch.createOrder"
	if dispatchSpan.Name() != wantName {
		t.Errorf("span name = %q, want %q", dispatchSpan.Name(), wantName)
	}
	attrs := map[string]string{}
	for _, kv := range dispatchSpan.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs["http.method"] != "POST" {
		t.Errorf("http.method = %q, want POST", attrs["http.method"])
	}
	if attrs["function.rid"] != "fn.createOrder" {
		t.Errorf("function.rid = %q, want fn.createOrder", attrs["function.rid"])
	}
	if !strings.Contains(attrs["http.url"], srv.URL) {
		t.Errorf("http.url %q does not contain test server %q", attrs["http.url"], srv.URL)
	}
	if attrs["http.status_code"] != "200" {
		t.Errorf("http.status_code = %q, want 200", attrs["http.status_code"])
	}
	// SpanKind: the dispatch span represents an outbound client call.
	// trace.SpanKindClient == 3 in the otel API (mirrored from the
	// sdk-trace ReadOnlySpan via the public alias). Tested via the
	// trace API constant rather than a magic number.
	if dispatchSpan.SpanKind() != otelTraceClientKind() {
		t.Errorf("span kind = %v, want client", dispatchSpan.SpanKind())
	}
}

// otelTraceClientKind returns the SpanKindClient constant from the
// API package, kept in a helper so the import line in this test file
// stays compact.
func otelTraceClientKind() otelTraceSpanKind {
	return otelTraceSpanKindClient
}

func TestBDD_HTTPDispatcher_5xxFlipsSpanToError(t *testing.T) {
	original := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(original) })
	recorder := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"db down"}`))
	}))
	t.Cleanup(srv.Close)

	d := NewHTTPDispatcher(srv.URL)
	_, err := d.Dispatch(context.Background(), &oms.ActionType{
		APIName:     "createOrder",
		RID:         "ri.action.main.createOrder",
		FunctionRID: "fn.createOrder",
	}, map[string]interface{}{})
	if err == nil {
		t.Fatal("Dispatch must return an error on 5xx")
	}

	var dispatchSpan trace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if strings.HasPrefix(s.Name(), "function.dispatch") {
			dispatchSpan = s
		}
	}
	if dispatchSpan == nil {
		t.Fatal("no function.dispatch span recorded")
	}
	if dispatchSpan.Status().Code != 1 { // codes.Error = 1
		t.Errorf("span status = %v, want Error — head-sampler will drop failing traces at low rates if status is not Error",
			dispatchSpan.Status().Code)
	}
	// Attributes should still carry the 5xx status code so dashboards
	// can filter on http.status_code=500.
	for _, kv := range dispatchSpan.Attributes() {
		if string(kv.Key) == "http.status_code" && kv.Value.Emit() != "500" {
			t.Errorf("http.status_code = %q, want 500", kv.Value.Emit())
		}
	}
}

func TestBDD_HTTPDispatcher_NoParentSpanStillInjectsTraceparent(t *testing.T) {
	// Even without an active parent the dispatcher span itself
	// becomes the root and its TraceContext must flow downstream so
	// the function server can correlate its log/trace lines back to
	// the action that triggered it.
	original := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(original) })
	tp := trace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("traceparent")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"edits":[]}`))
	}))
	t.Cleanup(srv.Close)

	_, err := NewHTTPDispatcher(srv.URL).Dispatch(context.Background(), &oms.ActionType{
		APIName:     "createOrder",
		RID:         "ri.action.main.createOrder",
		FunctionRID: "fn.createOrder",
	}, map[string]interface{}{})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if captured == "" {
		t.Fatal("traceparent absent — dispatcher span did not propagate even as a root")
	}
}
