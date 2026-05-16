//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/functions"
	"github.com/liyang/weave/pkg/oms"
)

// US-475 — Function 版本表 + 确定性 hash 重放 (BDD).
//
// Two scenarios drive the PRD's "纯函数 100% 一致；引用时间的函数标记
// non-deterministic" verbatim against:
//   - real testcontainers PostgreSQL for both the OMS function row and
//     the function_executions audit log;
//   - real Goja runtime (pkg/functions.NewRuntime), so the time-referencing
//     scenario produces non-determinism naturally via Date.now() rather
//     than via a returns-N-then-M test double;
//   - real chi router + real OMSHandler /execute + /replay endpoints.
//
// Scenario A (pure) proves the deterministic happy path: identical hash,
// HTTP 200, deterministic=true, originalOutput == replayOutput, and the
// replay row pinned back at the original execution via replay_of.
//
// Scenario B (time-referencing) proves the WEAVE_FUNCTION_NONDETERMINISTIC
// detection: HTTP 409, deterministic=false, originalOutput.now and
// replayOutput.now both populated but different, both PG rows persisted
// with diverging output hashes — i.e. the audit log captures both legs
// for future drift investigations.

// us475GojaExecutor adapts a *functions.Runtime onto oms.FunctionExecutor.
// Production wiring lives in cmd/server (or a follow-up story); for the
// BDD the adapter just forwards through.
type us475GojaExecutor struct{ rt *functions.Runtime }

func (g *us475GojaExecutor) Execute(ctx context.Context, fn *oms.Function, params map[string]interface{}) (interface{}, error) {
	return g.rt.Execute(ctx, fn.SourceCode, map[string]interface{}{
		"parameters": params,
	})
}

// us475PGExecStore is a compact mirror of cmd/server's pgFunctionExecutionStore.
// Inlining keeps the BDD self-contained without pulling cmd/server (package
// main) into the integration_test scope.
type us475PGExecStore struct{ pool *pgxpool.Pool }

func (s *us475PGExecStore) RecordExecution(ctx context.Context, exec *oms.FunctionExecution) error {
	if exec == nil || exec.ExecutionID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO function_executions
		   (execution_id, function_rid, function_name, function_version, ontology_rid,
		    input_hash, output_hash, input_json, output_json, error_message,
		    requested_by, is_replay, replay_of, executed_at)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		 ON CONFLICT (execution_id) DO NOTHING`,
		exec.ExecutionID, exec.FunctionRID, exec.FunctionName, exec.FunctionVersion, exec.OntologyRID,
		exec.InputHash, exec.OutputHash, us475CoerceJSON(exec.InputJSON), us475CoerceJSON(exec.OutputJSON), exec.ErrorMessage,
		exec.RequestedBy, exec.IsReplay, exec.ReplayOf, exec.ExecutedAt,
	)
	return err
}

func (s *us475PGExecStore) GetExecution(ctx context.Context, executionID string) (*oms.FunctionExecution, error) {
	if executionID == "" {
		return nil, oms.ErrExecutionNotFound
	}
	row := s.pool.QueryRow(ctx,
		`SELECT execution_id, function_rid, function_name, function_version, ontology_rid,
		        input_hash, output_hash, input_json, output_json, error_message,
		        requested_by, is_replay, replay_of, executed_at
		 FROM function_executions WHERE execution_id = $1`, executionID)
	return us475ScanRow(row)
}

func (s *us475PGExecStore) FindByInputHash(ctx context.Context, fnRID, version, inputHash string) (*oms.FunctionExecution, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT execution_id, function_rid, function_name, function_version, ontology_rid,
		        input_hash, output_hash, input_json, output_json, error_message,
		        requested_by, is_replay, replay_of, executed_at
		 FROM function_executions
		 WHERE function_rid=$1 AND function_version=$2 AND input_hash=$3
		 ORDER BY executed_at DESC LIMIT 1`, fnRID, version, inputHash)
	return us475ScanRow(row)
}

func (s *us475PGExecStore) ListExecutions(ctx context.Context, fnRID, version string, limit int) ([]*oms.FunctionExecution, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT execution_id, function_rid, function_name, function_version, ontology_rid,
		        input_hash, output_hash, input_json, output_json, error_message,
		        requested_by, is_replay, replay_of, executed_at
		 FROM function_executions
		 WHERE function_rid=$1 AND ($2='' OR function_version=$2)
		 ORDER BY executed_at DESC LIMIT $3`, fnRID, version, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*oms.FunctionExecution
	for rows.Next() {
		exec, err := us475ScanRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, exec)
	}
	return out, rows.Err()
}

func us475ScanRow(r pgx.Row) (*oms.FunctionExecution, error) {
	exec := &oms.FunctionExecution{}
	var inputJSON, outputJSON []byte
	if err := r.Scan(
		&exec.ExecutionID, &exec.FunctionRID, &exec.FunctionName, &exec.FunctionVersion, &exec.OntologyRID,
		&exec.InputHash, &exec.OutputHash, &inputJSON, &outputJSON, &exec.ErrorMessage,
		&exec.RequestedBy, &exec.IsReplay, &exec.ReplayOf, &exec.ExecutedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, oms.ErrExecutionNotFound
		}
		return nil, err
	}
	exec.InputJSON = inputJSON
	exec.OutputJSON = outputJSON
	return exec, nil
}

func us475CoerceJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	return raw
}

// setupUS475Fixture is the shared setup for both scenarios — fresh PG +
// migrations + ontology + function row + chi router with /execute, the
// ontology-scoped /replay, and the US-475 top-level /api/v2/functions/{rid}/replay.
func setupUS475Fixture(t *testing.T, sourceCode, fnRIDSuffix, fnName string) (
	router *chi.Mux,
	store *us475PGExecStore,
	fn *oms.Function,
	ontologyAPIName string,
) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	repo := oms.NewPGRepository(pg.Pool)

	ont := &oms.Ontology{
		RID:         "ri.ontology.main.ontology.us475-bdd",
		APIName:     "us475-bdd",
		DisplayName: "US-475 BDD",
	}
	if err := repo.CreateOntology(context.Background(), ont); err != nil {
		t.Fatalf("create ontology: %v", err)
	}

	fn = &oms.Function{
		RID:         "ri.ontology.main.function." + fnRIDSuffix,
		OntologyRID: ont.RID,
		Name:        fnName,
		Version:     "1.0.0",
		SourceCode:  sourceCode,
		Runtime:     "goja",
		Signature: json.RawMessage(
			`{"params":[{"name":"a","type":"integer","required":true},` +
				`{"name":"b","type":"integer","required":true}],` +
				`"returns":{"type":"object"}}`),
	}
	if err := repo.CreateFunction(context.Background(), fn); err != nil {
		t.Fatalf("create function: %v", err)
	}

	handler := oms.NewOMSHandler(repo)
	handler.SetFunctionExecutor(&us475GojaExecutor{rt: functions.NewRuntime(functions.DefaultConfig())})
	store = &us475PGExecStore{pool: pg.Pool}
	handler.SetFunctionExecutionStore(store)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", handler.ExecuteFunction)
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/replay", handler.ReplayFunction)
	r.Post("/api/v2/functions/{functionRid}/replay", handler.ReplayFunctionByRID)

	return r, store, fn, ont.APIName
}

// TestBDD_US475_PureFunction_DeterministicReplay covers PRD acceptance:
// "纯函数 100% 一致" — through a real Goja runtime, a real PG repo, and
// the real /execute + /replay handlers. Uses the US-475 top-level alias
// path (/api/v2/functions/{rid}/replay) so both the response shape AND
// the new endpoint surface get exercised end-to-end.
func TestBDD_US475_PureFunction_DeterministicReplay(t *testing.T) {
	router, store, fn, ontAPI := setupUS475Fixture(t,
		`function main(input) {
           return { sum: input.parameters.a + input.parameters.b };
         }`,
		"us475-pure", "addPure")

	// --- When: operator executes the function once via ontology-scoped path
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/functions/"+fn.RID+"/execute",
		bytes.NewBufferString(`{"parameters":{"a":3,"b":4}}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("execute failed: %d body=%s", w.Code, w.Body.String())
	}

	rows, err := store.ListExecutions(context.Background(), fn.RID, "1.0.0", 1)
	if err != nil || len(rows) == 0 {
		t.Fatalf("list executions: rows=%d err=%v", len(rows), err)
	}
	originalID := rows[0].ExecutionID

	// --- When: operator replays via the US-475 PRD-literal top-level path
	body, _ := json.Marshal(map[string]string{"executionId": originalID})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/functions/"+fn.RID+"/replay",
		bytes.NewBuffer(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 deterministic replay, got %d body=%s", w.Code, w.Body.String())
	}

	// --- Then: deterministic=true, originalOutput == replayOutput, hashes match
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if det, _ := resp["deterministic"].(bool); !det {
		t.Errorf("expected deterministic=true on pure function, got %v", resp["deterministic"])
	}
	orig, _ := resp["originalOutput"].(map[string]interface{})
	rep, _ := resp["replayOutput"].(map[string]interface{})
	if got, _ := orig["sum"].(float64); got != 7 {
		t.Errorf("originalOutput.sum: got %v want 7 (raw=%v)", orig["sum"], orig)
	}
	if got, _ := rep["sum"].(float64); got != 7 {
		t.Errorf("replayOutput.sum: got %v want 7 (raw=%v)", rep["sum"], rep)
	}
	if resp["originalHash"] != resp["replayHash"] {
		t.Errorf("hashes should match on deterministic replay, got %v vs %v",
			resp["originalHash"], resp["replayHash"])
	}

	// --- Then: persisted PG replay row links to the original execution
	allRows, _ := store.ListExecutions(context.Background(), fn.RID, "1.0.0", 10)
	if len(allRows) < 2 {
		t.Fatalf("expected ≥2 persisted rows after replay, got %d", len(allRows))
	}
	if !allRows[0].IsReplay || allRows[0].ReplayOf != originalID {
		t.Errorf("expected replay row pointing at %q, got %+v", originalID, allRows[0])
	}
}

// TestBDD_US475_TimeReferencingFunction_FlagsNonDeterministic exercises the
// PRD's "引用时间的函数标记 non-deterministic" verbatim. The function calls
// Date.now() — Goja honours it via the host runtime, so two consecutive
// invocations land on different milliseconds (with a deterministic spin
// loop seeding ≥1ms between calls to guarantee divergence even on the
// fastest CI worker). The replay endpoint MUST flag the drift.
func TestBDD_US475_TimeReferencingFunction_FlagsNonDeterministic(t *testing.T) {
	router, store, fn, ontAPI := setupUS475Fixture(t,
		// The spin loop runs Date.now() until the ms advances at least
		// once — guarantees the first execute and the subsequent replay
		// land on different ms values even on a host that completes the
		// JS body sub-millisecond. Capped at 1e7 iterations as a safety.
		`function main(input) {
           var start = Date.now();
           var spin = 0;
           while (Date.now() === start) { spin++; if (spin > 10000000) break; }
           return { now: Date.now(), sum: input.parameters.a + input.parameters.b };
         }`,
		"us475-time", "addWithTime")

	// --- When: operator executes once ---
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/functions/"+fn.RID+"/execute",
		bytes.NewBufferString(`{"parameters":{"a":3,"b":4}}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("execute failed: %d body=%s", w.Code, w.Body.String())
	}
	rows, err := store.ListExecutions(context.Background(), fn.RID, "1.0.0", 1)
	if err != nil || len(rows) == 0 {
		t.Fatalf("list executions: rows=%d err=%v", len(rows), err)
	}
	originalID := rows[0].ExecutionID

	// --- When: operator replays — Date.now() returns a different ms ---
	body, _ := json.Marshal(map[string]string{"executionId": originalID})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+ontAPI+"/functions/"+fn.RID+"/replay",
		bytes.NewBuffer(body)))

	// --- Then: 409 + deterministic=false + WEAVE_FUNCTION_NONDETERMINISTIC
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 from time-referencing function replay, got %d body=%s",
			w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if det, _ := resp["deterministic"].(bool); det {
		t.Errorf("expected deterministic=false on time-referencing fn, got true")
	}
	warning, ok := resp["warning"].(map[string]interface{})
	if !ok || warning["code"] != "WEAVE_FUNCTION_NONDETERMINISTIC" {
		t.Fatalf("expected WEAVE_FUNCTION_NONDETERMINISTIC warning, got %+v",
			resp["warning"])
	}

	// originalOutput / replayOutput both surfaced; sum stable, now diverges
	orig, _ := resp["originalOutput"].(map[string]interface{})
	rep, _ := resp["replayOutput"].(map[string]interface{})
	if got, _ := orig["sum"].(float64); got != 7 {
		t.Errorf("originalOutput.sum: got %v want 7", orig["sum"])
	}
	if got, _ := rep["sum"].(float64); got != 7 {
		t.Errorf("replayOutput.sum: got %v want 7", rep["sum"])
	}
	origNow, _ := orig["now"].(float64)
	repNow, _ := rep["now"].(float64)
	if origNow == 0 || repNow == 0 {
		t.Errorf("expected Date.now() readings populated, got original=%v replay=%v",
			origNow, repNow)
	}
	if origNow == repNow {
		t.Errorf("expected Date.now() to differ between execute and replay (deterministic spin should advance ≥1ms), got identical %v", origNow)
	}

	// --- Then: PG audit row for the replay leg is persisted with the new hash
	allRows, _ := store.ListExecutions(context.Background(), fn.RID, "1.0.0", 10)
	if len(allRows) < 2 {
		t.Fatalf("expected ≥2 persisted rows after replay, got %d", len(allRows))
	}
	if !allRows[0].IsReplay || allRows[0].ReplayOf != originalID {
		t.Errorf("expected replay row pointing at %q, got %+v", originalID, allRows[0])
	}
	if allRows[0].OutputHash == allRows[1].OutputHash {
		t.Errorf("expected diverging output hashes in PG audit log, got both = %q",
			allRows[0].OutputHash)
	}
}
