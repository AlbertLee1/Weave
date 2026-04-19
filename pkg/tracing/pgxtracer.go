package tracing

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// maxSQLAttributeLen caps the SQL stamped on the span so a 1MB
// generated INSERT doesn't dominate exporter payloads.
const maxSQLAttributeLen = 4096

// PgxTracer is a pgx.QueryTracer implementation that wraps every
// Query / QueryRow / Exec call in an OpenTelemetry span. Wired in
// cmd/server/main.go via the pgxpool.Config.ConnConfig.Tracer hook so
// every PG-backed repo method (regardless of which package owns it)
// gets a span without per-method instrumentation.
type PgxTracer struct{}

// NewPgxTracer returns a ready-to-use tracer. Trivial constructor kept
// for symmetry with future option-bearing variants.
func NewPgxTracer() *PgxTracer { return &PgxTracer{} }

// TraceQueryStart starts a span around a single PG query. The first
// keyword of the SQL (SELECT / INSERT / UPDATE / DELETE / ...) is used
// as a stable suffix in the span name so all SELECTs roll up cleanly
// in trace UIs.
func (t *PgxTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if t == nil {
		return ctx
	}
	op := firstSQLKeyword(data.SQL)
	spanName := "db.query"
	if op != "" {
		spanName = "db.query " + op
	}
	stmt := data.SQL
	if len(stmt) > maxSQLAttributeLen {
		stmt = stmt[:maxSQLAttributeLen]
	}
	ctx, _ = otel.Tracer(tracerName).Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.statement", stmt),
			attribute.String("db.operation", op),
		),
	)
	return ctx
}

// TraceQueryEnd ends the span started by TraceQueryStart. Errors are
// recorded on the span; CommandTag (rows affected) is stamped as an
// attribute when present.
func (t *PgxTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if t == nil {
		return
	}
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	if data.Err != nil {
		span.RecordError(data.Err)
		span.SetStatus(codes.Error, data.Err.Error())
	} else if affected := data.CommandTag.RowsAffected(); affected > 0 {
		span.SetAttributes(attribute.Int64("db.rows_affected", affected))
	}
	span.End()
}

// firstSQLKeyword returns the first whitespace-delimited token of a
// SQL statement, upper-cased. Whitespace includes leading newlines
// from heredoc-style queries. Empty input returns "".
func firstSQLKeyword(sql string) string {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return ""
	}
	if idx := strings.IndexAny(trimmed, " \t\n\r"); idx > 0 {
		return strings.ToUpper(trimmed[:idx])
	}
	return strings.ToUpper(trimmed)
}
