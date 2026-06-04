package sqlqueries_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/liyang/weave/pkg/sqlqueries"
)

// resultEngine is a fake that implements the optional ResultEngine
// interface so the handler can inline columns + rows on the succeeded
// QueryStatus. It records the query it was asked to run.
type resultEngine struct {
	gotQuery string
	columns  []string
	rows     [][]any
	err      error
}

func (e *resultEngine) Execute(_ context.Context, query string) error {
	e.gotQuery = query
	return e.err
}

func (e *resultEngine) ExecuteWithResult(_ context.Context, query string) ([]string, [][]any, error) {
	e.gotQuery = query
	if e.err != nil {
		return nil, nil, e.err
	}
	return e.columns, e.rows, nil
}

// TestExecute_Succeeded_ReturnsColumnsAndRows pins the contract that a
// succeeded QueryStatus carries the result payload (columns + rows) when
// the engine implements ResultEngine.
func TestExecute_Succeeded_ReturnsColumnsAndRows(t *testing.T) {
	engine := &resultEngine{
		columns: []string{"id", "name"},
		rows: [][]any{
			{float64(1), "alpha"},
			{float64(2), "beta"},
		},
	}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "SELECT id, name FROM t",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp sqlqueries.QueryStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Type != "succeeded" {
		t.Fatalf("type = %q, want succeeded", resp.Type)
	}
	if len(resp.Columns) != 2 || resp.Columns[0] != "id" || resp.Columns[1] != "name" {
		t.Fatalf("columns = %v, want [id name]", resp.Columns)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("rows len = %d, want 2", len(resp.Rows))
	}
	if resp.Rows[0][1] != "alpha" {
		t.Fatalf("rows[0][1] = %v, want alpha", resp.Rows[0][1])
	}
}

// TestExecute_Succeeded_EmptyResultStillSucceeds verifies a zero-row
// SELECT returns columns (so the UI can render the header) and an empty
// rows slice, not a null payload.
func TestExecute_Succeeded_EmptyResultStillSucceeds(t *testing.T) {
	engine := &resultEngine{
		columns: []string{"id"},
		rows:    [][]any{},
	}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "SELECT id FROM t WHERE false",
	})
	var resp sqlqueries.QueryStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Type != "succeeded" {
		t.Fatalf("type = %q, want succeeded", resp.Type)
	}
	if len(resp.Columns) != 1 || resp.Columns[0] != "id" {
		t.Fatalf("columns = %v, want [id]", resp.Columns)
	}
	if resp.Rows == nil {
		t.Fatalf("rows is nil, want non-nil empty slice")
	}
	if len(resp.Rows) != 0 {
		t.Fatalf("rows len = %d, want 0", len(resp.Rows))
	}
}

// TestExecute_ResultEngine_MaxRowsExceeded confirms the row-cap sentinel
// from the result path still maps to the failed/MaxRowsExceeded envelope
// without leaking partial rows.
func TestExecute_ResultEngine_MaxRowsExceeded(t *testing.T) {
	engine := &resultEngine{err: sqlqueries.ErrMaxRowsExceeded}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "SELECT generate_series(1, 1000000)",
	})
	var resp sqlqueries.QueryStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Type != "failed" {
		t.Fatalf("type = %q, want failed", resp.Type)
	}
	if resp.FailureReason != "MaxRowsExceeded" {
		t.Fatalf("failureReason = %q, want MaxRowsExceeded", resp.FailureReason)
	}
	if resp.Columns != nil || resp.Rows != nil {
		t.Fatalf("failed response must not carry columns/rows, got cols=%v rows=%v", resp.Columns, resp.Rows)
	}
}

// TestExecute_PlainEngine_NoResultPayload guarantees backward
// compatibility: an engine that only implements Execute (no
// ResultEngine) still succeeds, just without an inlined result payload.
func TestExecute_PlainEngine_NoResultPayload(t *testing.T) {
	engine := &fakeEngine{}
	r := newRouter(engine)

	rec := doPost(t, r, "/api/v2/sqlQueries/execute", map[string]interface{}{
		"query": "SELECT 1",
	})
	var resp sqlqueries.QueryStatus
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Type != "succeeded" {
		t.Fatalf("type = %q, want succeeded", resp.Type)
	}
	if resp.Columns != nil || resp.Rows != nil {
		t.Fatalf("plain engine must not synthesize a result payload, got cols=%v rows=%v", resp.Columns, resp.Rows)
	}
}

// TestPGEngine_ImplementsResultEngine is a compile-time assertion that
// PGEngine satisfies the optional ResultEngine interface.
func TestPGEngine_ImplementsResultEngine(t *testing.T) {
	var _ sqlqueries.ResultEngine = (*sqlqueries.PGEngine)(nil)
	// Sanity: the safety sentinel is still distinct from result errors.
	if errors.Is(sqlqueries.ErrMaxRowsExceeded, sqlqueries.ErrForbiddenStatement) {
		t.Fatal("ErrMaxRowsExceeded must not alias ErrForbiddenStatement")
	}
}
