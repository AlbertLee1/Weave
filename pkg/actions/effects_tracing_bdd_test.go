package actions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestBDD_Webhook_TraceContextPropagation closes the second outbound-
// HTTP trace-snap point on the action-execution path (round 52 fixed
// the function-server HTTPDispatcher; this round mirrors the same
// fix for the side-effect webhook dispatcher).
//
// Before this round pkg/actions/effects.go did not thread ctx through
// dispatchSingleEffect, executeWebhookEffectTracked, or
// doWebhookAttempt, so:
//   - The webhook request used http.NewRequest (no parent context —
//     cancellation from the upstream action could not flow through).
//   - The propagator was never invoked, so the receiver saw an empty
//     traceparent and started a fresh root span. Operators could not
//     correlate "webhook receiver was slow" with the action that
//     triggered it.
//
// Scenarios:
//   - Inject scenario: with a parent span active the webhook server
//     receives a traceparent header whose trace-id matches the parent.
//   - Span scenario: each attempt produces a client-kind span named
//     "sideeffect.webhook" with http.method / http.url /
//     http.status_code / sideeffect.attempt attributes.
//   - 5xx scenario: a 500 response on the final attempt flips the
//     span status to Error so head-sampled traces keep the failing
//     attempt.
//   - Retry scenario: a transient 503 followed by 200 produces TWO
//     spans (one per attempt) and the second one is the success.

func TestBDD_Webhook_InjectsTraceContextOnOutboundCall(t *testing.T) {
	original := otel.GetTracerProvider()
	originalProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(original)
		otel.SetTextMapPropagator(originalProp)
	})
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var captured string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = r.Header.Get("traceparent")
		mu.Unlock()
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	cfgJSON, _ := json.Marshal(webhookConfig{URL: srv.URL})
	effects, _ := json.Marshal([]SideEffect{{Type: "webhook", Config: cfgJSON}})

	tracer := tp.Tracer("test")
	ctx, parent := tracer.Start(context.Background(), "parent.test")
	defer parent.End()

	outcomes, _, err := ExecuteSideEffectsWithOutcomesCtx(ctx, effects, ActionResult{ActionRID: "rid-1"})
	if err != nil {
		t.Fatalf("ExecuteSideEffectsWithOutcomesCtx: %v", err)
	}
	if len(outcomes) != 1 || outcomes[0].Status != SideEffectStatusSuccess {
		t.Fatalf("outcome = %+v, want success", outcomes)
	}
	mu.Lock()
	tp_str := captured
	mu.Unlock()
	if tp_str == "" {
		t.Fatal("webhook server received empty traceparent — propagator was not invoked")
	}
	parts := strings.Split(tp_str, "-")
	if len(parts) != 4 {
		t.Fatalf("malformed traceparent %q", tp_str)
	}
	parentTraceHex := parent.SpanContext().TraceID().String()
	if parts[1] != parentTraceHex {
		t.Errorf("trace-id=%s does not match parent=%s — webhook server will see a disjoint trace tree", parts[1], parentTraceHex)
	}
}

func TestBDD_Webhook_EmitsClientSpanWithAttributes(t *testing.T) {
	original := otel.GetTracerProvider()
	originalProp := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(original)
		otel.SetTextMapPropagator(originalProp)
	})
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	cfgJSON, _ := json.Marshal(webhookConfig{URL: srv.URL})
	effects, _ := json.Marshal([]SideEffect{{Type: "webhook", Config: cfgJSON}})

	if _, _, err := ExecuteSideEffectsWithOutcomesCtx(context.Background(), effects, ActionResult{ActionRID: "rid-1"}); err != nil {
		t.Fatalf("ExecuteSideEffectsWithOutcomesCtx: %v", err)
	}

	var attemptSpans []sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == "sideeffect.webhook" {
			attemptSpans = append(attemptSpans, s)
		}
	}
	if len(attemptSpans) != 1 {
		t.Fatalf("got %d sideeffect.webhook spans, want 1", len(attemptSpans))
	}
	span := attemptSpans[0]
	attrs := map[string]string{}
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs["http.method"] != "POST" {
		t.Errorf("http.method = %q, want POST", attrs["http.method"])
	}
	if !strings.Contains(attrs["http.url"], srv.URL) {
		t.Errorf("http.url = %q does not contain %q", attrs["http.url"], srv.URL)
	}
	if attrs["http.status_code"] != "200" {
		t.Errorf("http.status_code = %q, want 200", attrs["http.status_code"])
	}
	if attrs["sideeffect.attempt"] != "1" {
		t.Errorf("sideeffect.attempt = %q, want 1", attrs["sideeffect.attempt"])
	}
	if span.SpanKind() != otelTraceSpanKindClient {
		t.Errorf("span kind = %v, want client", span.SpanKind())
	}
}

func TestBDD_Webhook_5xxFinalAttemptFlipsSpanToError(t *testing.T) {
	original := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(original) })
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	t.Cleanup(srv.Close)

	cfgJSON, _ := json.Marshal(webhookConfig{
		URL:                      srv.URL,
		MaxRetries:               1,
		RetryBackoffMilliseconds: 1, // tiny backoff so the test stays fast
	})
	effects, _ := json.Marshal([]SideEffect{{Type: "webhook", Config: cfgJSON}})
	outcomes, _, _ := ExecuteSideEffectsWithOutcomesCtx(context.Background(), effects, ActionResult{ActionRID: "rid-1"})
	if len(outcomes) != 1 || outcomes[0].Status != SideEffectStatusFailed {
		t.Fatalf("outcome = %+v, want failed", outcomes)
	}

	var spans []sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == "sideeffect.webhook" {
			spans = append(spans, s)
		}
	}
	// 1 initial + 1 retry = 2 attempts -> 2 spans, last one is failure.
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2 (initial + 1 retry)", len(spans))
	}
	final := spans[1]
	if final.Status().Code != 1 { // codes.Error = 1
		t.Errorf("final span status = %v, want Error", final.Status().Code)
	}
}

func TestBDD_Webhook_TransientFollowedBySuccessProducesTwoSpans(t *testing.T) {
	original := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(original) })
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	var count int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		count++
		c := count
		mu.Unlock()
		if c == 1 {
			w.WriteHeader(503) // transient
			return
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)

	cfgJSON, _ := json.Marshal(webhookConfig{
		URL:                      srv.URL,
		MaxRetries:               2,
		RetryBackoffMilliseconds: 1,
	})
	effects, _ := json.Marshal([]SideEffect{{Type: "webhook", Config: cfgJSON}})
	outcomes, _, _ := ExecuteSideEffectsWithOutcomesCtx(context.Background(), effects, ActionResult{ActionRID: "rid-1"})
	if outcomes[0].Status != SideEffectStatusSuccess {
		t.Fatalf("outcome = %+v, want success", outcomes)
	}

	var spans []sdktrace.ReadOnlySpan
	for _, s := range recorder.Ended() {
		if s.Name() == "sideeffect.webhook" {
			spans = append(spans, s)
		}
	}
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2 (failed retry + success)", len(spans))
	}
	// First span: attempt 1, status 503, Error.
	first := spans[0]
	attrs1 := map[string]string{}
	for _, kv := range first.Attributes() {
		attrs1[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs1["sideeffect.attempt"] != "1" || attrs1["http.status_code"] != "503" {
		t.Errorf("first span attempt/status = %q/%q, want 1/503", attrs1["sideeffect.attempt"], attrs1["http.status_code"])
	}
	if first.Status().Code != 1 {
		t.Errorf("first span status = %v, want Error", first.Status().Code)
	}
	// Second span: attempt 2, status 200, success.
	second := spans[1]
	attrs2 := map[string]string{}
	for _, kv := range second.Attributes() {
		attrs2[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs2["sideeffect.attempt"] != "2" || attrs2["http.status_code"] != "200" {
		t.Errorf("second span attempt/status = %q/%q, want 2/200", attrs2["sideeffect.attempt"], attrs2["http.status_code"])
	}
}
