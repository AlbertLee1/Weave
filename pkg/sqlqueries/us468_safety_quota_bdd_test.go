//go:build integration

package sqlqueries_test

// US-468 BDD integration coverage for the SQL Queries sandbox.
//
// The story closes three contracts:
//   1. Read-only enforcement — DML / DDL never reach the PG planner.
//   2. Per-query timeout (default 5s, configurable) — a slow statement
//      aborts with ErrQueryTimeout / failureReason="QueryTimeout".
//   3. Result row cap (default 10K, configurable) — once exceeded the
//      stream is cut with ErrMaxRowsExceeded / failureReason=
//      "MaxRowsExceeded". The PG-side statement itself does not need
//      to finish; the engine pulls the rip-cord as soon as the cap
//      trips, and the read-only tx rolls back any side-effects.
//
// All scenarios are end-to-end: testcontainers PG → chi router → real
// SqlQueries handler → engine → pgxpool, exactly the path a wire client
// would take. No mocks below this file's setup helpers.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/sqlqueries"
)

func us468Route(t *testing.T, engine sqlqueries.Engine) *chi.Mux {
	t.Helper()
	r := chi.NewRouter()
	sqlqueries.NewHandler(engine).RegisterRoutes(r)
	return r
}

func us468Post(t *testing.T, r http.Handler, query string) sqlqueries.QueryStatus {
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
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}
	return resp
}

// TestBDD_SqlQueries_Given_DMLAndDDLPayloads_When_Execute_Then_RejectedBeforeDB
// closes acceptance criterion 1 + 3: every DML / DDL form is rejected
// at validation time, so the PG pool never sees the statement.
func TestBDD_SqlQueries_Given_DMLAndDDLPayloads_When_Execute_Then_RejectedBeforeDB(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	engine := sqlqueries.NewPGEngineWithConfig(pg.Pool, sqlqueries.DefaultConfig())
	r := us468Route(t, engine)

	// Seed a sentinel row in a table we never DROP — if any of these
	// statements reached the planner the table would either disappear,
	// mutate, or vanish.
	ctx := context.Background()
	if _, err := pg.Pool.Exec(ctx, `CREATE TABLE us468_witness (id int PRIMARY KEY, n int NOT NULL)`); err != nil {
		t.Fatalf("seed table: %v", err)
	}
	if _, err := pg.Pool.Exec(ctx, `INSERT INTO us468_witness (id, n) VALUES (1, 100)`); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	cases := []struct {
		name           string
		query          string
		wantReason     string
		wantSubstring  string
	}{
		{"INSERT rejected", "INSERT INTO us468_witness (id, n) VALUES (2, 200)", "NonSelectQuery", "INSERT"},
		{"UPDATE rejected", "UPDATE us468_witness SET n = 0 WHERE id = 1", "NonSelectQuery", "UPDATE"},
		{"DELETE rejected", "DELETE FROM us468_witness WHERE id = 1", "NonSelectQuery", "DELETE"},
		{"DROP rejected", "DROP TABLE us468_witness", "NonSelectQuery", "DROP"},
		{"TRUNCATE rejected", "TRUNCATE us468_witness", "NonSelectQuery", "TRUNCATE"},
		{"ALTER rejected", "ALTER TABLE us468_witness ADD COLUMN x int", "NonSelectQuery", "ALTER"},
		{"CREATE rejected", "CREATE TABLE us468_other (id int)", "NonSelectQuery", "CREATE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Validation-rejected payloads return HTTP 400 from the
			// handler, not a QueryStatus envelope. Re-issue the call
			// here so we can assert the body shape directly.
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

	// Witness invariant: row 1 still has n=100 and no extra rows arrived.
	var count int
	if err := pg.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM us468_witness`).Scan(&count); err != nil {
		t.Fatalf("witness count: %v", err)
	}
	if count != 1 {
		t.Fatalf("witness row count = %d, want 1 — a DML payload bypassed validation", count)
	}
	var n int
	if err := pg.Pool.QueryRow(ctx, `SELECT n FROM us468_witness WHERE id = 1`).Scan(&n); err != nil {
		t.Fatalf("witness row read: %v", err)
	}
	if n != 100 {
		t.Fatalf("witness row n = %d, want 100 — an UPDATE payload bypassed validation", n)
	}
}

// TestBDD_SqlQueries_Given_SlowQueryAndShortTimeout_When_Execute_Then_FailsWithQueryTimeout
// closes acceptance criterion 2: a query that exceeds Config.Timeout is
// aborted and surfaced as QueryStatus{type=failed, failureReason=QueryTimeout}.
// pg_sleep is the canonical wall-clock fixture but the validator blocks
// any `pg_*` identifier, so we drive the timeout via a large generate_series
// scan instead — the planner needs hundreds of seconds for 1B rows, well
// past any practical Timeout configured in this test.
func TestBDD_SqlQueries_Given_SlowQueryAndShortTimeout_When_Execute_Then_FailsWithQueryTimeout(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	cfg := sqlqueries.Config{Timeout: 200 * time.Millisecond, MaxRows: sqlqueries.DefaultMaxRows}
	engine := sqlqueries.NewPGEngineWithConfig(pg.Pool, cfg)
	r := us468Route(t, engine)

	start := time.Now()
	resp := us468Post(t, r, "SELECT count(*) FROM generate_series(1, 1000000000) AS g")
	elapsed := time.Since(start)

	if resp.Type != "failed" {
		t.Fatalf("type = %q, want failed — full resp=%+v", resp.Type, resp)
	}
	if resp.FailureReason != "QueryTimeout" {
		t.Fatalf("failureReason = %q, want QueryTimeout — full resp=%+v", resp.FailureReason, resp)
	}
	// Timeout must actually bite within a reasonable multiple of the
	// configured budget. 2s leaves slack for PG cancel handshake but
	// is far below the 5s pg_sleep argument that would otherwise run.
	if elapsed > 2*time.Second {
		t.Fatalf("elapsed = %v, want under 2s (timeout was %v but query slept ~5s before cancel)", elapsed, cfg.Timeout)
	}

	// Engine-level errors.Is wiring: same path, exposed as a sentinel
	// so SDK callers can branch without parsing the wire reason.
	ctx := context.Background()
	err := engine.Execute(ctx, "SELECT count(*) FROM generate_series(1, 1000000000) AS g")
	if err == nil {
		t.Fatalf("engine.Execute returned nil — want ErrQueryTimeout")
	}
	if !errors.Is(err, sqlqueries.ErrQueryTimeout) {
		t.Fatalf("engine.Execute err = %v, want errors.Is ErrQueryTimeout", err)
	}
}

// TestBDD_SqlQueries_Given_LargeResultAndLowMaxRows_When_Execute_Then_FailsWithMaxRowsExceeded
// closes acceptance criterion 2 / row cap: a SELECT that streams more
// than Config.MaxRows rows aborts before pulling the entire result set.
// generate_series is the canonical CPU-cheap, row-heavy test fixture.
func TestBDD_SqlQueries_Given_LargeResultAndLowMaxRows_When_Execute_Then_FailsWithMaxRowsExceeded(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	cfg := sqlqueries.Config{Timeout: sqlqueries.DefaultQueryTimeout, MaxRows: 10}
	engine := sqlqueries.NewPGEngineWithConfig(pg.Pool, cfg)
	r := us468Route(t, engine)

	resp := us468Post(t, r, "SELECT generate_series(1, 1000)")
	if resp.Type != "failed" {
		t.Fatalf("type = %q, want failed — full resp=%+v", resp.Type, resp)
	}
	if resp.FailureReason != "MaxRowsExceeded" {
		t.Fatalf("failureReason = %q, want MaxRowsExceeded — full resp=%+v", resp.FailureReason, resp)
	}

	// At the boundary (exactly MaxRows = 10 rows), the engine must
	// still succeed — the cap is "more than MaxRows", not "≥".
	resp = us468Post(t, r, "SELECT generate_series(1, 10)")
	if resp.Type != "succeeded" {
		t.Fatalf("boundary case type = %q, want succeeded — full resp=%+v", resp.Type, resp)
	}

	// Engine-level errors.Is wiring.
	ctx := context.Background()
	err := engine.Execute(ctx, "SELECT generate_series(1, 1000)")
	if err == nil {
		t.Fatalf("engine.Execute returned nil — want ErrMaxRowsExceeded")
	}
	if !errors.Is(err, sqlqueries.ErrMaxRowsExceeded) {
		t.Fatalf("engine.Execute err = %v, want errors.Is ErrMaxRowsExceeded", err)
	}
}

// TestBDD_SqlQueries_Given_DefaultConfig_When_SmallQuery_Then_Succeeds is the
// happy-path complement: when the query is well within both quotas the
// engine returns the Foundry "succeeded" status. Without this BDD case
// the timeout/cap tests could pass trivially against a broken engine
// that fails every call.
func TestBDD_SqlQueries_Given_DefaultConfig_When_SmallQuery_Then_Succeeds(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	engine := sqlqueries.NewPGEngine(pg.Pool)
	r := us468Route(t, engine)

	resp := us468Post(t, r, "SELECT 1")
	if resp.Type != "succeeded" {
		t.Fatalf("type = %q, want succeeded — full resp=%+v", resp.Type, resp)
	}
	if resp.QueryID == "" {
		t.Fatalf("queryId is empty on succeeded path")
	}

	// generate_series(1, 100) is well below the 10K default cap.
	resp = us468Post(t, r, "SELECT generate_series(1, 100)")
	if resp.Type != "succeeded" {
		t.Fatalf("100-row query type = %q, want succeeded — full resp=%+v", resp.Type, resp)
	}
}
