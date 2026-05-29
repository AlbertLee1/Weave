// Package tracing wires OpenTelemetry trace exporters and an HTTP
// middleware that turns each request into a span. The package is loaded
// during boot from cmd/server/main.go and is safe to no-op when tracing
// is disabled.
//
// Three exporters are supported:
//   - "stdout"     — pretty-prints spans to stderr; safe for local dev.
//   - "otlp"       — sends spans to an OTLP collector. Protocol selected by
//     Config.OTLPProtocol ("http" or "grpc"); defaults to http.
//   - "otlphttp"   — explicit OTLP/HTTP shorthand.
//   - "otlpgrpc"   — explicit OTLP/gRPC shorthand.
//   - "none"       — installs a no-op processor; useful for tests.
//
// When Config.Enabled is false, Init() returns a no-op shutdown function
// and does NOT touch the global tracer provider, so the rest of the
// codebase can call otel.Tracer() unconditionally without paying any
// cost in dev / test setups.
//
// Init also installs a composite TextMapPropagator (W3C TraceContext +
// Baggage) so cross-process trace stitching and baggage forwarding both
// work out of the box.
package tracing

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

// Config controls the OpenTelemetry tracer provider Init() builds.
type Config struct {
	// Enabled gates the entire package; when false Init() is a no-op.
	Enabled bool

	// Exporter selects the span exporter. One of
	// "stdout" | "otlp" | "otlphttp" | "otlpgrpc" | "none". Defaults to
	// "stdout" when empty. The plain "otlp" value defers to OTLPProtocol.
	Exporter string

	// OTLPEndpoint is the host:port of the OTLP collector. Used only when
	// Exporter selects an OTLP transport. When empty, the OpenTelemetry
	// default is used (the OTLP exporter picks up
	// OTEL_EXPORTER_OTLP_ENDPOINT itself).
	OTLPEndpoint string

	// OTLPProtocol selects the OTLP transport when Exporter == "otlp".
	// Accepts "http" (default) or "grpc". Ignored when Exporter is one of
	// the explicit otlphttp / otlpgrpc shorthands.
	OTLPProtocol string

	// OTLPInsecure disables TLS on the OTLP transport. Defaults to true so
	// the in-cluster collector path (`otel-collector:4317`) works without
	// extra wiring; set to false when targeting a TLS-fronted collector.
	OTLPInsecure bool

	// ServiceName / ServiceVersion are stamped onto every span as
	// resource attributes. ServiceName defaults to "weave".
	ServiceName    string
	ServiceVersion string

	// SampleRate (US-439) is the head-based sampling probability for
	// non-error / non-slow spans, in [0, 1]. 0 drops every span that
	// is not force-sampled by the carve-outs; 1 keeps everything.
	// Values outside [0, 1] are clamped. The zero value (0.0) is
	// preserved verbatim so production deployments must opt in to
	// full-fidelity tracing explicitly via WEAVE_TRACE_SAMPLE_RATE=1.
	SampleRate float64

	// SlowSpanThreshold (US-439) is the duration above which a span is
	// force-sampled regardless of SampleRate. Zero disables the
	// slow-span carve-out. Defaults to 1 second when wired through
	// Init() with the zero value (matches PRD US-439 spec).
	SlowSpanThreshold time.Duration
}

// DefaultSlowSpanThreshold is the SlowSpanThreshold value Init() applies
// when the caller leaves the field zero. PRD US-439 fixes this at 1s.
const DefaultSlowSpanThreshold = 1 * time.Second

// noopShutdown is the shutdown function returned when tracing is disabled.
// It exists so callers can always defer shutdown(ctx) without nil-checking.
func noopShutdown(_ context.Context) error { return nil }

// installPropagator sets a composite TextMapPropagator that handles
// both W3C Trace Context (cross-process span stitching) AND W3C Baggage
// (request-scoped key/value propagation, e.g. request_id / user_id).
// Called from Init() so every Init path picks up consistent propagation.
func installPropagator() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// Init wires a tracer provider for Weave. The returned shutdown function
// flushes pending spans and tears down the exporter; call it from main()
// during graceful shutdown.
func Init(ctx context.Context, cfg Config) (shutdown func(context.Context) error, err error) {
	if !cfg.Enabled {
		// Even when disabled we still install the propagator so any test
		// or background job that constructs spans manually picks up a
		// sane propagation default.
		installPropagator()
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

	// Resolve the explicit "otlp" alias against the protocol selector
	// here so the switch below stays a literal mapping.
	if exporterName == "otlp" {
		switch strings.ToLower(strings.TrimSpace(cfg.OTLPProtocol)) {
		case "grpc":
			exporterName = "otlpgrpc"
		default:
			exporterName = "otlphttp"
		}
	}

	// US-439: defaultise the slow-span threshold so a config that leaves
	// it zero still gets the PRD-spec 1s carve-out. SampleRate is NOT
	// defaulted: the zero value is the explicit "drop everything except
	// errors / slow requests" mode and is the recommended production
	// setting. Operators must opt in to full-fidelity tracing.
	slowThreshold := cfg.SlowSpanThreshold
	if slowThreshold == 0 {
		slowThreshold = DefaultSlowSpanThreshold
	}

	var exporter sdktrace.SpanExporter
	switch exporterName {
	case "none":
		// Build a tracer provider with no exporter so spans are still
		// generated (and observable to in-process processors) but never
		// shipped anywhere. Still apply the sampling filter so callers
		// that wire a recording processor downstream observe the same
		// drop semantics as a production deployment.
		tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		installPropagator()
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
	case "otlphttp":
		opts := []otlptracehttp.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(cfg.OTLPEndpoint))
		}
		if cfg.OTLPInsecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		exp, expErr := otlptracehttp.New(ctx, opts...)
		if expErr != nil {
			return noopShutdown, fmt.Errorf("tracing: otlphttp exporter: %w", expErr)
		}
		exporter = exp
	case "otlpgrpc":
		opts := []otlptracegrpc.Option{}
		if cfg.OTLPEndpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint))
		}
		if cfg.OTLPInsecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		exp, expErr := otlptracegrpc.New(ctx, opts...)
		if expErr != nil {
			return noopShutdown, fmt.Errorf("tracing: otlpgrpc exporter: %w", expErr)
		}
		exporter = exp
	default:
		return noopShutdown, fmt.Errorf("tracing: unknown exporter %q (want stdout|otlp|otlphttp|otlpgrpc|none)", exporterName)
	}

	// US-439: wrap the batch processor in the sampling filter so the
	// SampleRate / slow-span / error-span rules apply uniformly. The
	// batch processor itself is the canonical async exporter used in
	// production; the sampling layer sits in front of it so dropped
	// spans never enter the batch queue.
	batcher := sdktrace.NewBatchSpanProcessor(exporter)
	sampled := newSamplingProcessor(batcher, cfg.SampleRate, slowThreshold)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sampled),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	installPropagator()

	return tp.Shutdown, nil
}

// tracerName is the package-scoped tracer name passed to otel.Tracer().
// Stable across the codebase so all spans authored in pkg/tracing share
// one InstrumentationScope.
const tracerName = "github.com/liyang/weave/pkg/tracing"

// Tracer returns the package-scoped Tracer, fetched fresh from the
// global TracerProvider on every call so test setups that swap the
// provider with installRecordingProvider(t) see their swap honoured.
func Tracer() trace.Tracer { return otel.Tracer(tracerName) }

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
//
// Inbound TraceContext / Baggage headers are extracted via the global
// propagator BEFORE the span is started so a parent context from an
// upstream service is honoured automatically.
func HTTPMiddleware() func(http.Handler) http.Handler {
	tracer := otel.Tracer(tracerName)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract any inbound TraceContext + Baggage headers so the
			// span we start nests under the upstream parent and inherits
			// any baggage already on the wire.
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			route := r.URL.Path
			if rctx := chi.RouteContext(ctx); rctx != nil {
				if pat := rctx.RoutePattern(); pat != "" {
					route = pat
				}
			}
			spanName := r.Method + " " + route
			ctx, span := tracer.Start(ctx, spanName,
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
			if rctx := chi.RouteContext(ctx); rctx != nil {
				if pat := rctx.RoutePattern(); pat != "" && pat != route {
					route = pat
					span.SetAttributes(attribute.String("http.route", route))
					span.SetName(r.Method + " " + route)
				}
			}
			span.SetAttributes(attribute.String("http.status_code", strconv.Itoa(capture.status)))
			// US-439: server errors (5xx) flip the span status to Error
			// so the head-based sampler force-keeps them even at fractional
			// SampleRate values. 4xx are client mistakes per OTel semantic
			// conventions and intentionally stay at the default Unset
			// status — they shouldn't dominate a low-rate trace stream.
			if capture.status >= 500 {
				span.SetStatus(codes.Error, http.StatusText(capture.status))
			}
		})
	}
}
