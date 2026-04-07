// Package tracing wires OpenTelemetry trace exporters and an HTTP
// middleware that turns each request into a span. The package is loaded
// during boot from cmd/server/main.go and is safe to no-op when tracing
// is disabled.
//
// Three exporters are supported:
//   - "stdout"  — pretty-prints spans to stderr; safe for local dev.
//   - "otlp"    — sends spans to an OTLP HTTP collector (Jaeger, Tempo, ...).
//   - "none"    — installs a no-op processor; useful for tests.
//
// When Config.Enabled is false, Init() returns a no-op shutdown function
// and does NOT touch the global tracer provider, so the rest of the
// codebase can call otel.Tracer() unconditionally without paying any
// cost in dev / test setups.
package tracing

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// Config controls the OpenTelemetry tracer provider Init() builds.
type Config struct {
	// Enabled gates the entire package; when false Init() is a no-op.
	Enabled bool

	// Exporter selects the span exporter. One of "stdout" | "otlp" | "none".
	// Defaults to "stdout" when empty.
	Exporter string

	// OTLPEndpoint is the host:port of the OTLP/HTTP collector. Used only
	// when Exporter == "otlp". When empty, the OpenTelemetry default is
	// used (otlptracehttp picks up OTEL_EXPORTER_OTLP_ENDPOINT itself).
	OTLPEndpoint string

	// ServiceName / ServiceVersion are stamped onto every span as
	// resource attributes. ServiceName defaults to "weave".
	ServiceName    string
	ServiceVersion string
}

// noopShutdown is the shutdown function returned when tracing is disabled.
// It exists so callers can always defer shutdown(ctx) without nil-checking.
func noopShutdown(_ context.Context) error { return nil }

// Init wires a tracer provider for Weave. The returned shutdown function
// flushes pending spans and tears down the exporter; call it from main()
// during graceful shutdown.
func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	if !cfg.Enabled {
		return noopShutdown, nil
	}

	exporterName := cfg.Exporter
	if exporterName == "" {
		exporterName = "stdout"
	}

	if cfg.ServiceName == "" {
		cfg.ServiceName = "weave"
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return noopShutdown, fmt.Errorf("tracing: build resource: %w", err)
	}

	var exporter sdktrace.SpanExporter
	switch exporterName {
	case "none":
		// Build a tracer provider with no exporter so spans are still
		// generated (and observable to in-process processors) but never
		// shipped anywhere.
		tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		return tp.Shutdown, nil
	case "stdout":
		exp, expErr := stdouttrace.New(
			stdouttrace.WithWriter(os.Stderr),
			stdouttrace.WithPrettyPrint(),
		)
		if expErr != nil {
			return noopShutdown, fmt.Errorf("tracing: stdout exporter: %w", expErr)
		}
		exporter = exp
	case "otlp":
		opts := []otlptracehttp.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(cfg.OTLPEndpoint), otlptracehttp.WithInsecure())
		}
		exp, expErr := otlptracehttp.New(ctx, opts...)
		if expErr != nil {
			return noopShutdown, fmt.Errorf("tracing: otlp exporter: %w", expErr)
		}
		exporter = exp
	default:
		return noopShutdown, fmt.Errorf("tracing: unknown exporter %q (want stdout|otlp|none)", exporterName)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}

// statusCapturingResponseWriter mirrors the helper in pkg/metrics so the
// tracing middleware can read back the status code.
type statusCapturingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusCapturingResponseWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusCapturingResponseWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// HTTPMiddleware returns a chi-compatible middleware that wraps each
// incoming request in a span tagged with http.method, http.route, and
// http.status_code. The span name is the chi route template (or the
// request path when no template matches) so cardinality stays bounded.
func HTTPMiddleware() func(http.Handler) http.Handler {
	tracer := otel.Tracer("github.com/liyang/weave/pkg/tracing")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := r.URL.Path
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pat := rctx.RoutePattern(); pat != "" {
					route = pat
				}
			}
			spanName := r.Method + " " + route
			ctx, span := tracer.Start(r.Context(), spanName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.route", route),
				),
			)
			defer span.End()

			capture := &statusCapturingResponseWriter{ResponseWriter: w, status: 0}
			next.ServeHTTP(capture, r.WithContext(ctx))

			if capture.status == 0 {
				capture.status = http.StatusOK
			}

			// Recompute route in case chi populated it after handling.
			if rctx := chi.RouteContext(r.Context()); rctx != nil {
				if pat := rctx.RoutePattern(); pat != "" && pat != route {
					route = pat
					span.SetAttributes(attribute.String("http.route", route))
					span.SetName(r.Method + " " + route)
				}
			}
			span.SetAttributes(attribute.String("http.status_code", strconv.Itoa(capture.status)))
		})
	}
}
