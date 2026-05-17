package oms_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-475 — Function 版本表 + 确定性 hash 重放.
//
// PRD 验收点 (priority 17):
//   - POST /api/v2/functions/{rid}/replay 返回 {deterministic, originalOutput, replayOutput}
//   - 纯函数 100% 一致；引用时间的函数标记 non-deterministic
//
// These tests pin down the **PRD-literal** shape that US-370 left out:
//   1. canonical response carries `deterministic` (boolean), `originalOutput`
//      (decoded JSON of the historical result) and `replayOutput` (decoded JSON
//      of the fresh result), in addition to the legacy {match, original,
//      result} triple.
//   2. a top-level `POST /api/v2/functions/{rid}/replay` path resolves the
//      Function by RID without an ontology prefix — matching the PRD path
//      literal so SDK clients that hold a Function RID don't need to also
//      remember the ontology api name.
//   3. a function whose live executor produces a *different* output on the
//      replay leg (the PRD's "function references time" archetype) trips
//      deterministic=false + WEAVE_FUNCTION_NONDETERMINISTIC, and both the
//      original + replay outputs are surfaced verbatim so an auditor can
//      diff them without a second round-trip.

func setupReplayRouterWithTopLevel(repo oms.Repository, exec oms.FunctionExecutor, store oms.FunctionExecutionStore) *chi.Mux {
	handler := oms.NewOMSHandler(repo)
	if exec != nil {
		handler.SetFunctionExecutor(exec)
	}
	if store != nil {
		handler.SetFunctionExecutionStore(store)
	}
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", handler.ExecuteFunction)
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/replay", handler.ReplayFunction)
	// PRD-literal top-level alias under US-475.
	r.Post("/api/v2/functions/{functionRid}/replay", handler.ReplayFunctionByRID)
	return r
}

// TestReplayUS475_ResponseIncludesCanonicalFields pins down the PRD wire shape:
// the canonical replay response must carry {deterministic, originalOutput,
// replayOutput} alongside the legacy {match, result, original} keys.
func TestReplayUS475_ResponseIncludesCanonicalFields(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(7), float64(7)}}
	store := &memExecutionStore{}
	router := setupReplayRouterWithTopLevel(repo, exec, store)

	// 1. Record an execution.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/execute",
		`{"parameters":{"a":3,"b":4}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("execute failed: %d %s", w.Code, w.Body.String())
	}
	originalID := store.rows[0].ExecutionID

	// 2. Replay deterministically — should expose canonical fields.
	body, _ := json.Marshal(map[string]string{"executionId": originalID})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/replay", string(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 deterministic replay, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	det, ok := resp["deterministic"].(bool)
	if !ok {
		t.Fatalf("expected canonical bool field `deterministic`, got %#v (response=%s)", resp["deterministic"], w.Body.String())
	}
	if !det {
		t.Errorf("expected deterministic=true on matching hash, got false")
	}
	// originalOutput must equal the recorded output (float64(7) → JSON 7).
	if got := resp["originalOutput"]; got == nil {
		t.Errorf("expected originalOutput populated when replaying a captured execution, got nil")
	} else if gotFloat, _ := got.(float64); gotFloat != 7 {
		t.Errorf("expected originalOutput=7, got %#v", got)
	}
	if got := resp["replayOutput"]; got == nil {
		t.Errorf("expected replayOutput populated, got nil")
	} else if gotFloat, _ := got.(float64); gotFloat != 7 {
		t.Errorf("expected replayOutput=7, got %#v", got)
	}
}

// TestReplayUS475_NondeterministicSurfacesBothOutputs replicates the PRD's
// "function references time" archetype: the executor returns a different
// value on the replay leg, so deterministic=false and both outputs MUST
// appear so the auditor can diff them.
func TestReplayUS475_NondeterministicSurfacesBothOutputs(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{
		map[string]interface{}{"now": float64(1700000000000), "sum": float64(7)},
		map[string]interface{}{"now": float64(1700000005000), "sum": float64(7)},
	}}
	store := &memExecutionStore{}
	router := setupReplayRouterWithTopLevel(repo, exec, store)

	// 1. Record a row.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/execute",
		`{"parameters":{"a":3,"b":4}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("execute failed: %d body=%s", w.Code, w.Body.String())
	}
	originalID := store.rows[0].ExecutionID

	// 2. Replay → divergent output.
	body, _ := json.Marshal(map[string]string{"executionId": originalID})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/replay", string(body)))
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 on hash divergence, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if det, _ := resp["deterministic"].(bool); det {
		t.Errorf("expected deterministic=false on hash divergence, got true")
	}
	warning, ok := resp["warning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected warning object, got %#v", resp["warning"])
	}
	if warning["code"] != "WEAVE_FUNCTION_NONDETERMINISTIC" {
		t.Errorf("expected WEAVE_FUNCTION_NONDETERMINISTIC code, got %v", warning["code"])
	}

	// originalOutput is the FIRST executor return → time=1700000000000.
	orig, ok := resp["originalOutput"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected originalOutput object, got %#v", resp["originalOutput"])
	}
	if got, _ := orig["now"].(float64); got != 1700000000000 {
		t.Errorf("expected originalOutput.now=1700000000000, got %v", orig["now"])
	}

	// replayOutput is the SECOND executor return → time=1700000005000.
	rep, ok := resp["replayOutput"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected replayOutput object, got %#v", resp["replayOutput"])
	}
	if got, _ := rep["now"].(float64); got != 1700000005000 {
		t.Errorf("expected replayOutput.now=1700000005000, got %v", rep["now"])
	}

	// Hashes must reflect the divergence too — replayHash must equal the
	// canonical hash of replayOutput (so SDK consumers can hash the bytes
	// themselves and compare).
	wantReplayHash := oms.HashFunctionOutput(map[string]interface{}{
		"now": float64(1700000005000), "sum": float64(7),
	})
	if resp["replayHash"] != wantReplayHash {
		t.Errorf("replayHash mismatch: got %v want %v", resp["replayHash"], wantReplayHash)
	}
}

// TestReplayUS475_TopLevelRIDPath proves the PRD-literal `/api/v2/functions/
// {rid}/replay` path works without an ontology prefix. SDKs that carry only
// a Function RID (e.g. from a stored ApplyResult) shouldn't have to round-trip
// to OMS just to learn the ontology api name.
func TestReplayUS475_TopLevelRIDPath(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(7), float64(7)}}
	store := &memExecutionStore{}
	router := setupReplayRouterWithTopLevel(repo, exec, store)

	// Seed the captured execution via the ontology path.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/execute",
		`{"parameters":{"a":3,"b":4}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("execute failed: %d body=%s", w.Code, w.Body.String())
	}
	originalID := store.rows[0].ExecutionID
	fnRID := store.rows[0].FunctionRID

	// Replay via top-level RID-only path — must succeed.
	body, _ := json.Marshal(map[string]string{"executionId": originalID})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/functions/"+fnRID+"/replay", string(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from top-level replay, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if det, _ := resp["deterministic"].(bool); !det {
		t.Errorf("expected deterministic=true, got %v", resp["deterministic"])
	}
	if resp["functionRid"] != fnRID {
		t.Errorf("expected functionRid=%q, got %v", fnRID, resp["functionRid"])
	}
}

// TestReplayUS475_TopLevelRequiresRIDShapedRef rejects bare-name refs at the
// top-level endpoint because there is no ontology context to disambiguate.
// `name@version` and bare names belong to the ontology-scoped path; the
// top-level path is RID-only.
func TestReplayUS475_TopLevelRequiresRIDShapedRef(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(7)}}
	store := &memExecutionStore{}
	router := setupReplayRouterWithTopLevel(repo, exec, store)

	// "add" is a bare name, NOT a RID. Top-level path must 400/404 it.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/functions/add/replay",
		`{"version":"1.0.0","input":{"a":1,"b":2}}`))
	if w.Code == http.StatusOK {
		t.Fatalf("expected non-200 for non-RID ref at top-level replay, got %d body=%s", w.Code, w.Body.String())
	}
}
