package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// StartSpan opens a new span on the package-scoped Tracer and returns
// the derived context plus the span. Convention for callers in
// pkg/oms / pkg/oss / pkg/actions:
//
//	ctx, span := tracing.StartSpan(ctx, "oms.GetOntology",
//	    attribute.String("ontology.api_name", apiName),
//	)
//	defer span.End()
//
// When tracing is disabled the global TracerProvider is the SDK's no-op
// implementation, so `span.End()` is free and the StartSpan call is a
// single allocation. Wrapping every business-path entry point therefore
// has effectively zero cost in dev and test setups where the exporter
// isn't wired.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{trace.WithSpanKind(trace.SpanKindInternal)}
	if len(attrs) > 0 {
		opts = append(opts, trace.WithAttributes(attrs...))
	}
	return otel.Tracer(tracerName).Start(ctx, name, opts...)
}

// RecordError stamps an error on a span and flips its status to Error
// without ending it, mirroring the OpenTelemetry "record + status"
// idiom. No-op on nil error so call sites can dispatch unconditionally.
func RecordError(span trace.Span, err error) {
	if err == nil || span == nil || !span.IsRecording() {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
