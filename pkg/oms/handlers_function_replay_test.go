package oms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-370: Function deterministic replay + version binding.
// The tests below exercise the hash determinism contract end-to-end via
// the handler surface so future drift between the registry-side
// (HashFunctionCode / HashFunctionSignature) and the runtime-side
// (HashFunctionInput / HashFunctionOutput / replay) hashing is caught.

type stubReplayExecutor struct {
	mu      sync.Mutex
	calls   int
	results []interface{}
	errs    []error
}

func (s *stubReplayExecutor) Execute(_ context.Context, fn *oms.Function, params map[string]interface{}) (interface{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.calls
	s.calls++
	if idx < len(s.errs) && s.errs[idx] != nil {
		return nil, s.errs[idx]
	}
	if idx < len(s.results) {
		return s.results[idx], nil
	}
	if len(s.results) > 0 {
		return s.results[len(s.results)-1], nil
	}
	_ = params
	_ = fn
	return nil, nil
}

type memExecutionStore struct {
	mu   sync.Mutex
	rows []*oms.FunctionExecution
}

func (m *memExecutionStore) RecordExecution(_ context.Context, exec *oms.FunctionExecution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	clone := *exec
	m.rows = append(m.rows, &clone)
	return nil
}

func (m *memExecutionStore) GetExecution(_ context.Context, executionID string) (*oms.FunctionExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.ExecutionID == executionID {
			clone := *row
			return &clone, nil
		}
	}
	return nil, oms.ErrExecutionNotFound
}

func (m *memExecutionStore) FindByInputHash(_ context.Context, fnRID, version, inputHash string) (*oms.FunctionExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.rows) - 1; i >= 0; i-- {
		row := m.rows[i]
		if row.FunctionRID == fnRID && row.FunctionVersion == version && row.InputHash == inputHash {
			clone := *row
			return &clone, nil
		}
	}
	return nil, oms.ErrExecutionNotFound
}

func (m *memExecutionStore) ListExecutions(_ context.Context, fnRID, version string, limit int) ([]*oms.FunctionExecution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*oms.FunctionExecution, 0, len(m.rows))
	for i := len(m.rows) - 1; i >= 0; i-- {
		row := m.rows[i]
		if row.FunctionRID != fnRID {
			continue
		}
		if version != "" && row.FunctionVersion != version {
			continue
		}
		clone := *row
		out = append(out, &clone)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func newReplayFixtureRepo() *mockRepo {
	return &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
		functions: []oms.Function{{
			RID:         "ri.ontology.main.function.add",
			OntologyRID: "ri.ontology.main.ontology.o1",
			Name:        "add",
			Version:     "1.0.0",
			SourceCode:  "function main(input){ return input.parameters.a + input.parameters.b }",
			Runtime:     "goja",
			Signature:   json.RawMessage(`{"params":[{"name":"a","type":"integer","required":true},{"name":"b","type":"integer","required":true}],"returns":{"type":"integer"}}`),
		}},
	}
}

func setupReplayRouter(repo oms.Repository, exec oms.FunctionExecutor, store oms.FunctionExecutionStore) (*chi.Mux, *oms.OMSHandler) {
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
	return r, handler
}

func doRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestReplay_RecordsExecutionOnExecute(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(7)}}
	store := &memExecutionStore{}
	router, _ := setupReplayRouter(repo, exec, store)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/execute",
		`{"parameters":{"a":3,"b":4}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if len(store.rows) != 1 {
		t.Fatalf("expected 1 persisted execution, got %d", len(store.rows))
	}
	row := store.rows[0]
	if row.IsReplay {
		t.Errorf("expected first execution to be live (not replay)")
	}
	if row.OutputHash == "" || row.InputHash == "" {
		t.Errorf("expected hashes populated, got input=%q output=%q", row.InputHash, row.OutputHash)
	}
	if row.OutputHash != oms.HashFunctionOutput(float64(7)) {
		t.Errorf("output hash mismatch: row=%q want=%q", row.OutputHash, oms.HashFunctionOutput(float64(7)))
	}
}

func TestReplay_DeterministicMatch(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(7), float64(7)}}
	store := &memExecutionStore{}
	router, _ := setupReplayRouter(repo, exec, store)

	// First execute records a row.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/execute",
		`{"parameters":{"a":3,"b":4}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("execute failed: %d %s", w.Code, w.Body.String())
	}
	originalID := store.rows[0].ExecutionID

	// Replay against the captured row; executor returns the same value so
	// the hashes match.
	body, _ := json.Marshal(map[string]string{"executionId": originalID})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/replay", string(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on deterministic replay, got %d body=%s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["match"] != true {
		t.Errorf("expected match=true, got %+v", resp["match"])
	}
	if resp["originalHash"] != resp["replayHash"] {
		t.Errorf("expected originalHash == replayHash, got %v vs %v", resp["originalHash"], resp["replayHash"])
	}
	if len(store.rows) != 2 {
		t.Errorf("expected replay to persist a second row, got %d", len(store.rows))
	}
	if !store.rows[1].IsReplay || store.rows[1].ReplayOf != originalID {
		t.Errorf("expected second row to be replay of %s, got %+v", originalID, store.rows[1])
	}
}

func TestReplay_NondeterministicReturns409(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(7), float64(99)}}
	store := &memExecutionStore{}
	router, _ := setupReplayRouter(repo, exec, store)

	// Execute records a row hashed at 7.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/execute",
		`{"parameters":{"a":3,"b":4}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("execute failed: %d", w.Code)
	}
	originalID := store.rows[0].ExecutionID
	originalHash := store.rows[0].OutputHash

	// Replay; executor returns 99 → hash diverges.
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
	if resp["match"] != false {
		t.Errorf("expected match=false, got %+v", resp["match"])
	}
	if resp["originalHash"] != originalHash {
		t.Errorf("expected originalHash %q in response, got %v", originalHash, resp["originalHash"])
	}
	warning, ok := resp["warning"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected warning object, got %+v", resp["warning"])
	}
	if warning["code"] != "WEAVE_FUNCTION_NONDETERMINISTIC" {
		t.Errorf("expected WEAVE_FUNCTION_NONDETERMINISTIC, got %v", warning["code"])
	}
}

func TestReplay_ExecutionNotFound(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(7)}}
	store := &memExecutionStore{}
	router, _ := setupReplayRouter(repo, exec, store)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/replay",
		`{"executionId":"fnx-missing"}`))
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestReplay_StoreNotConfiguredReturns503(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(7)}}
	router, _ := setupReplayRouter(repo, exec, nil) // no store

	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/replay",
		`{"executionId":"fnx-anything"}`))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when store is nil, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestReplay_AdHocVersionAndInput(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(11)}}
	store := &memExecutionStore{}
	router, _ := setupReplayRouter(repo, exec, store)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/replay",
		`{"version":"1.0.0","input":{"a":5,"b":6}}`))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 ad-hoc replay, got %d body=%s", w.Code, w.Body.String())
	}
	if len(store.rows) != 1 || !store.rows[0].IsReplay {
		t.Errorf("expected single replay row, got %+v", store.rows)
	}
}

func TestReplay_RequiresVersionWhenNoExecutionID(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{results: []interface{}{float64(7)}}
	store := &memExecutionStore{}
	router, _ := setupReplayRouter(repo, exec, store)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/replay",
		`{"input":{"a":3,"b":4}}`))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when version + executionId both missing, got %d", w.Code)
	}
}

func TestHashFunctionCode_StableForSameSource(t *testing.T) {
	if oms.HashFunctionCode("function f() { return 1; }") != oms.HashFunctionCode("function f() { return 1; }") {
		t.Errorf("identical source should hash identically")
	}
	if oms.HashFunctionCode("function f() { return 1; }") == oms.HashFunctionCode("function f() { return 2; }") {
		t.Errorf("different source should hash differently")
	}
}

func TestHashFunctionInput_KeyOrderInvariant(t *testing.T) {
	a := map[string]interface{}{"x": 1, "y": 2}
	b := map[string]interface{}{"y": 2, "x": 1}
	if oms.HashFunctionInput(a) != oms.HashFunctionInput(b) {
		t.Errorf("hash should be stable across map key order")
	}
}

func TestHashFunctionSignature_EmptyShapesCollapse(t *testing.T) {
	a := oms.HashFunctionSignature(nil)
	b := oms.HashFunctionSignature([]byte("{}"))
	c := oms.HashFunctionSignature([]byte("null"))
	d := oms.HashFunctionSignature([]byte("  {}  "))
	if a != b || a != c || a != d {
		t.Errorf("empty signature shapes should hash identically: %q %q %q %q", a, b, c, d)
	}
}

func TestRecordExecution_SkippedOnExecutorError(t *testing.T) {
	repo := newReplayFixtureRepo()
	exec := &stubReplayExecutor{errs: []error{errors.New("boom")}}
	store := &memExecutionStore{}
	router, _ := setupReplayRouter(repo, exec, store)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, doRequest(http.MethodPost,
		"/api/v2/ontologies/northwind/functions/add/execute",
		`{"parameters":{"a":1,"b":2}}`))
	if w.Code == http.StatusOK {
		t.Fatalf("expected non-200 for executor error, got %d", w.Code)
	}
	if len(store.rows) != 1 {
		t.Fatalf("expected the failure row to still be recorded for audit, got %d", len(store.rows))
	}
	if store.rows[0].OutputHash != "" || store.rows[0].ErrorMessage == "" {
		t.Errorf("expected ErrorMessage populated and OutputHash empty for failed execution: %+v", store.rows[0])
	}
}
