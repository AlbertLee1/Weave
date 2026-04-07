package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestInit_Stdout_NoError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := Config{
		Enabled:        true,
		Exporter:       "stdout",
		ServiceName:    "weave-test",
		ServiceVersion: "v0.0.0-test",
	}
	shutdown, err := Init(ctx, cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown function")
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestInit_Disabled_NoError(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Enabled: false, ServiceName: "weave"}
	shutdown, err := Init(ctx, cfg)
	if err != nil {
		t.Fatalf("Init disabled: %v", err)
	}
	if shutdown == nil {
		t.Fatalf("expected non-nil shutdown even when disabled")
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestInit_None_NoError(t *testing.T) {
	ctx := context.Background()
	cfg := Config{Enabled: true, Exporter: "none", ServiceName: "weave"}
	shutdown, err := Init(ctx, cfg)
	if err != nil {
		t.Fatalf("Init none: %v", err)
	}
	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

// installRecordingProvider swaps the global tracer provider with one that
// records spans into a tracetest.SpanRecorder so we can assert on them.
// Returns a cleanup function that restores the previous provider.
func installRecordingProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := trace.NewTracerProvider(trace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})
	return rec
}

func TestHTTPMiddleware_CreatesSpan(t *testing.T) {
	rec := installRecordingProvider(t)

	mw := HTTPMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v2/things/42", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	if span.Name() == "" {
		t.Errorf("span name is empty")
	}

	// Verify standard HTTP attributes are present.
	attrs := map[string]string{}
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs["http.method"] != "GET" {
		t.Errorf("http.method: got %q, want GET", attrs["http.method"])
	}
}

func TestHTTPMiddleware_RecordsStatusCode(t *testing.T) {
	rec := installRecordingProvider(t)

	mw := HTTPMiddleware()
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	req := httptest.NewRequest(http.MethodGet, "/teapot", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	got := ""
	for _, kv := range span.Attributes() {
		if string(kv.Key) == "http.status_code" {
			got = kv.Value.Emit()
		}
	}
	if got != "418" {
		t.Errorf("http.status_code attribute: got %q, want 418", got)
	}
}
