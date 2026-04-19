package tracing

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPgxTracer_StartEndCreatesSpanWithAttributes(t *testing.T) {
	rec := installRecordingProvider(t)

	tr := NewPgxTracer()
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL: "SELECT id, name FROM ontologies WHERE id = $1",
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{
		CommandTag: pgconn.NewCommandTag("SELECT 1"),
	})

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	if span.Name() != "db.query SELECT" {
		t.Errorf("span name: got %q, want %q", span.Name(), "db.query SELECT")
	}
	attrs := map[string]string{}
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs["db.system"] != "postgresql" {
		t.Errorf("db.system: got %q, want postgresql", attrs["db.system"])
	}
	if attrs["db.operation"] != "SELECT" {
		t.Errorf("db.operation: got %q, want SELECT", attrs["db.operation"])
	}
	if attrs["db.statement"] == "" {
		t.Errorf("db.statement: expected non-empty")
	}
	if attrs["db.rows_affected"] != "1" {
		t.Errorf("db.rows_affected: got %q, want 1", attrs["db.rows_affected"])
	}
}

func TestPgxTracer_RecordsErrorOnFailedQuery(t *testing.T) {
	rec := installRecordingProvider(t)

	tr := NewPgxTracer()
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL: "INSERT INTO things VALUES ($1)",
	})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{
		Err: errors.New("duplicate key value"),
	})

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	span := spans[0]
	if span.Status().Code.String() != "Error" {
		t.Errorf("span status: got %q, want Error", span.Status().Code.String())
	}
	if len(span.Events()) == 0 {
		t.Errorf("expected at least one error event recorded on span")
	}
}

func TestPgxTracer_TruncatesLongStatement(t *testing.T) {
	rec := installRecordingProvider(t)

	long := make([]byte, maxSQLAttributeLen+512)
	for i := range long {
		long[i] = 'x'
	}

	tr := NewPgxTracer()
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: string(long)})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	for _, kv := range spans[0].Attributes() {
		if string(kv.Key) == "db.statement" {
			if got := len(kv.Value.AsString()); got != maxSQLAttributeLen {
				t.Errorf("db.statement length: got %d, want %d", got, maxSQLAttributeLen)
			}
		}
	}
}

func TestFirstSQLKeyword(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"SELECT * FROM t", "SELECT"},
		{"  insert into t values (1)", "INSERT"},
		{"\n\tUPDATE t SET x=1", "UPDATE"},
		{"DELETE FROM t", "DELETE"},
		{"VACUUM", "VACUUM"},
		{"", ""},
		{"   \n  ", ""},
	}
	for _, c := range cases {
		if got := firstSQLKeyword(c.in); got != c.want {
			t.Errorf("firstSQLKeyword(%q): got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPgxTracer_NilReceiverIsSafe(t *testing.T) {
	var tr *PgxTracer
	ctx := tr.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tr.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{}) // must not panic
}
