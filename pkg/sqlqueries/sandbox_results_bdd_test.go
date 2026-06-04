//go:build integration

package sqlqueries_test

// BDD integration coverage for the SQL sandbox result-row contract.
//
// The story closes the gap where the engine executed a query but threw
// away the column values, so the wire client could verify a SELECT ran
// but never see any data. These scenarios drive the full path —
// testcontainers PG → chi router → SqlQueries handler → PGEngine →
// pgxpool — and assert the succeeded QueryStatus now carries columns +
// rows, while the read-only / row-cap / safety guarantees are unchanged.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/sqlqueries"
)

func sandboxRoute(t *testing.T, engine sqlqueries.Engine) *chi.Mux {
	t.Helper()
	r := chi.NewRouter()
	sqlqueries.NewHandler(engine).RegisterRoutes(r)
	return r
}

func sandboxPost(t *testing.T, r http.Handler, query string) (int, sqlqueries.QueryStatus, []byte) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v2/sqlQueries/execute", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var resp sqlqueries.QueryStatus
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
		}
	}
	return rec.Code, resp, rec.Body.Bytes()
}

// TestBDD_SqlSandboxReturnsRows_Given_SeededTable_When_Select_Then_ColumnsAndRowsReturned
// is the headline scenario: a SELECT against a seeded table returns the
// column names and the corresponding row values in the succeeded status.
func TestBDD_SqlSandboxReturnsRows_Given_SeededTable_When_Select_Then_ColumnsAndRowsReturned(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	engine := sqlqueries.NewPGEngine(pg.Pool)
	r := sandboxRoute(t, engine)

	ctx := context.Background()
	if _, err := pg.Pool.Exec(ctx, `CREATE TABLE sandbox_people (id int PRIMARY KEY, name text NOT NULL, active boolean NOT NULL)`); err != nil {
		t.Fatalf("seed table: %v", err)
	}
	if _, err := pg.Pool.Exec(ctx, `INSERT INTO sandbox_people (id, name, active) VALUES (1, 'alice', true), (2, 'bob', false)`); err != nil {
		t.Fatalf("seed rows: %v", err)
	}

	code, resp, raw := sandboxPost(t, r, "SELECT id, name, active FROM sandbox_people ORDER BY id")
	if code != http.StatusOK {
		t.Fatalf("HTTP code = %d, want 200 — body=%s", code, raw)
	}
	if resp.Type != "succeeded" {
		t.Fatalf("type = %q, want succeeded — resp=%+v", resp.Type, resp)
	}
	wantCols := []string{"id", "name", "active"}
	if len(resp.Columns) != len(wantCols) {
		t.Fatalf("columns = %v, want %v", resp.Columns, wantCols)
	}
	for i, c := range wantCols {
		if resp.Columns[i] != c {
			t.Fatalf("columns[%d] = %q, want %q", i, resp.Columns[i], c)
		}
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("rows len = %d, want 2 — rows=%v", len(resp.Rows), resp.Rows)
	}
	// Row 1: id=1 (JSON number), name="alice", active=true.
	if got := jsonNumber(t, resp.Rows[0][0]); got != 1 {
		t.Fatalf("rows[0][0] (id) = %v, want 1", resp.Rows[0][0])
	}
	if resp.Rows[0][1] != "alice" {
		t.Fatalf("rows[0][1] (name) = %v, want alice", resp.Rows[0][1])
	}
	if resp.Rows[0][2] != true {
		t.Fatalf("rows[0][2] (active) = %v, want true", resp.Rows[0][2])
	}
	if resp.Rows[1][1] != "bob" {
		t.Fatalf("rows[1][1] (name) = %v, want bob", resp.Rows[1][1])
	}
}

// TestBDD_SqlSandboxReturnsRows_Given_EmptyResult_When_Select_Then_ColumnsButZeroRows
// confirms a zero-row SELECT still returns the column header (so the UI
// can render an empty table) and an empty — not null — rows array.
func TestBDD_SqlSandboxReturnsRows_Given_EmptyResult_When_Select_Then_ColumnsButZeroRows(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	engine := sqlqueries.NewPGEngine(pg.Pool)
	r := sandboxRoute(t, engine)

	code, resp, raw := sandboxPost(t, r, "SELECT 1 AS only_col WHERE false")
	if code != http.StatusOK {
		t.Fatalf("HTTP code = %d, want 200 — body=%s", code, raw)
	}
	if resp.Type != "succeeded" {
		t.Fatalf("type = %q, want succeeded — resp=%+v", resp.Type, resp)
	}
	if len(resp.Columns) != 1 || resp.Columns[0] != "only_col" {
		t.Fatalf("columns = %v, want [only_col]", resp.Columns)
	}
	if resp.Rows == nil {
		t.Fatalf("rows is nil, want empty array — raw=%s", raw)
	}
	if len(resp.Rows) != 0 {
		t.Fatalf("rows len = %d, want 0", len(resp.Rows))
	}
	// The wire body must carry "rows":[] not "rows":null for an empty set.
	if !bytes.Contains(raw, []byte(`"rows":[]`)) {
		t.Fatalf("wire body must contain \"rows\":[] for empty result, got %s", raw)
	}
}

// TestBDD_SqlSandboxReturnsRows_Given_LargeResultAndLowCap_When_Select_Then_MaxRowsExceededNoData
// confirms the row cap still bites on the result path: more than MaxRows
// rows aborts with failureReason=MaxRowsExceeded and the failed envelope
// carries no partial data.
func TestBDD_SqlSandboxReturnsRows_Given_LargeResultAndLowCap_When_Select_Then_MaxRowsExceededNoData(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	cfg := sqlqueries.Config{Timeout: sqlqueries.DefaultQueryTimeout, MaxRows: 10}
	engine := sqlqueries.NewPGEngineWithConfig(pg.Pool, cfg)
	r := sandboxRoute(t, engine)

	code, resp, raw := sandboxPost(t, r, "SELECT generate_series(1, 1000)")
	if code != http.StatusOK {
		t.Fatalf("HTTP code = %d, want 200 — body=%s", code, raw)
	}
	if resp.Type != "failed" {
		t.Fatalf("type = %q, want failed — resp=%+v", resp.Type, resp)
	}
	if resp.FailureReason != "MaxRowsExceeded" {
		t.Fatalf("failureReason = %q, want MaxRowsExceeded", resp.FailureReason)
	}
	if resp.Columns != nil || resp.Rows != nil {
		t.Fatalf("failed envelope must not carry data, got cols=%v rows=%v", resp.Columns, resp.Rows)
	}

	// Boundary: exactly MaxRows succeeds and returns all rows.
	code, resp, raw = sandboxPost(t, r, "SELECT generate_series(1, 10) AS g")
	if code != http.StatusOK || resp.Type != "succeeded" {
		t.Fatalf("boundary: code=%d type=%q, want 200/succeeded — body=%s", code, resp.Type, raw)
	}
	if len(resp.Rows) != 10 {
		t.Fatalf("boundary rows len = %d, want 10", len(resp.Rows))
	}
}

// TestBDD_SqlSandboxReturnsRows_Given_NonReadOnlySQL_When_Execute_Then_RejectedBySafety
// guards that the result-returning path did NOT weaken the safety gate:
// DML / DDL / system-table / stacked statements are still rejected at
// validation time (HTTP 400) before any PG round-trip, and the witness
// table is untouched.
func TestBDD_SqlSandboxReturnsRows_Given_NonReadOnlySQL_When_Execute_Then_RejectedBySafety(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	engine := sqlqueries.NewPGEngine(pg.Pool)
	r := sandboxRoute(t, engine)

	ctx := context.Background()
	if _, err := pg.Pool.Exec(ctx, `CREATE TABLE sandbox_witness (id int PRIMARY KEY, n int NOT NULL)`); err != nil {
		t.Fatalf("seed table: %v", err)
	}
	if _, err := pg.Pool.Exec(ctx, `INSERT INTO sandbox_witness (id, n) VALUES (1, 42)`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	cases := []struct {
		name       string
		query      string
		wantReason string
	}{
		{"DROP rejected", "DROP TABLE sandbox_witness", "NonSelectQuery"},
		{"UPDATE rejected", "UPDATE sandbox_witness SET n = 0 WHERE id = 1", "NonSelectQuery"},
		{"DELETE rejected", "DELETE FROM sandbox_witness WHERE id = 1", "NonSelectQuery"},
		{"stacked rejected", "SELECT 1; DROP TABLE sandbox_witness", "StackedStatement"},
		{"system table rejected", "SELECT * FROM pg_catalog.pg_tables", "SystemTableAccess"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost,
				"/api/v2/sqlQueries/execute",
				bytes.NewReader([]byte(`{"query":`+strconv.Quote(tc.query)+`}`)))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("HTTP code = %d, want 400 — body=%s", rec.Code, rec.Body.String())
			}
			if !bytes.Contains(rec.Body.Bytes(), []byte(tc.wantReason)) {
				t.Fatalf("body missing reason %q: %s", tc.wantReason, rec.Body.String())
			}
		})
	}

	// Witness invariant: nothing mutated.
	var n int
	if err := pg.Pool.QueryRow(ctx, `SELECT n FROM sandbox_witness WHERE id = 1`).Scan(&n); err != nil {
		t.Fatalf("witness read: %v", err)
	}
	if n != 42 {
		t.Fatalf("witness n = %d, want 42 — a non-read-only payload reached PG", n)
	}
}

// jsonNumber coerces a JSON-decoded numeric (float64) into an int for
// readable assertions. pgx returns PG int4 as int32 in-process, but after
// the JSON round-trip the wire client sees a float64.
func jsonNumber(t *testing.T, v any) int {
	t.Helper()
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	default:
		t.Fatalf("value %v (%T) is not numeric", v, v)
		return 0
	}
}
