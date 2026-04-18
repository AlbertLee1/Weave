package oms_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// US-216: POST /functions/{rid}/execute validates the caller's parameters
// against the function's declared signature BEFORE dispatching to a backing
// executor. The tests below assert the validator gates the dispatch path —
// missing required, type mismatch, and unknown-key all surface 400 with a
// structured `parameter`+`code` payload, while the happy path forwards the
// coerced map to the executor.

type stubFunctionExecutor struct {
	gotFn     *oms.Function
	gotParams map[string]interface{}
	result    interface{}
	err       error
}

func (s *stubFunctionExecutor) Execute(_ context.Context, fn *oms.Function, params map[string]interface{}) (interface{}, error) {
	s.gotFn = fn
	s.gotParams = params
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func setupFunctionExecuteRouter(repo oms.Repository, exec oms.FunctionExecutor) (*chi.Mux, *oms.OMSHandler) {
	handler := oms.NewOMSHandler(repo)
	if exec != nil {
		handler.SetFunctionExecutor(exec)
	}
	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", handler.ExecuteFunction)
	return r, handler
}

func newExecuteFixtureRepo(sig string) *mockRepo {
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
			Version:     1,
			SourceCode:  "function main(input){ return input.parameters.a + input.parameters.b }",
			Runtime:     "goja",
			Signature:   json.RawMessage(sig),
		}},
	}
}

func doExecute(t *testing.T, router *chi.Mux, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions/add/execute", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestExecuteFunction_HappyPath_ForwardsCoercedParams(t *testing.T) {
	sig := `{"params":[{"name":"a","type":"integer","required":true},{"name":"b","type":"integer","required":true}],"returns":{"type":"integer"}}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{result: float64(7)}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doExecute(t, router, `{"parameters":{"a":3,"b":4}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if exec.gotFn == nil || exec.gotFn.RID != "ri.ontology.main.function.add" {
		t.Fatalf("executor did not receive function, got %+v", exec.gotFn)
	}
	if exec.gotParams["a"] != float64(3) || exec.gotParams["b"] != float64(4) {
		t.Errorf("executor saw wrong params: %+v", exec.gotParams)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["result"] != float64(7) {
		t.Errorf("unexpected result: %+v", resp)
	}
}

func TestExecuteFunction_RejectsMissingRequired(t *testing.T) {
	sig := `{"params":[{"name":"a","type":"integer","required":true}]}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if exec.gotFn != nil {
		t.Fatal("executor should not be invoked when validation fails")
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	params, _ := body["parameters"].(map[string]interface{})
	if params["code"] != "missing_required" {
		t.Errorf("expected code=missing_required, got %+v", body)
	}
	if params["parameter"] != "a" {
		t.Errorf("expected parameter=a, got %+v", body)
	}
}

func TestExecuteFunction_RejectsTypeMismatch(t *testing.T) {
	sig := `{"params":[{"name":"a","type":"integer","required":true}]}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doExecute(t, router, `{"parameters":{"a":"oops"}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	if exec.gotFn != nil {
		t.Fatal("executor should not be invoked on type mismatch")
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	params, _ := body["parameters"].(map[string]interface{})
	if params["code"] != "type_mismatch" {
		t.Errorf("expected code=type_mismatch, got %+v", body)
	}
}

func TestExecuteFunction_RejectsUnknownParameter(t *testing.T) {
	sig := `{"params":[{"name":"a","type":"integer","required":true}]}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doExecute(t, router, `{"parameters":{"a":1,"extra":true}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	params, _ := body["parameters"].(map[string]interface{})
	if params["code"] != "unknown_parameter" {
		t.Errorf("expected code=unknown_parameter, got %+v", body)
	}
}

func TestExecuteFunction_AppliesDefaultsBeforeDispatch(t *testing.T) {
	sig := `{"params":[{"name":"limit","type":"integer","default":10}]}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{result: "ok"}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if exec.gotParams["limit"] != float64(10) {
		t.Errorf("expected default limit=10 forwarded, got %+v", exec.gotParams)
	}
}

func TestExecuteFunction_NoSignatureAcceptsAnyInput(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{result: "ok"}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doExecute(t, router, `{"parameters":{"x":1,"y":"foo","z":true}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if len(exec.gotParams) != 3 {
		t.Errorf("expected all 3 params forwarded, got %+v", exec.gotParams)
	}
}

func TestExecuteFunction_EmptyBodyAcceptedWhenNoRequiredParams(t *testing.T) {
	sig := `{"params":[{"name":"limit","type":"integer","default":5}]}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{result: "ok"}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions/add/execute", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if exec.gotParams["limit"] != float64(5) {
		t.Errorf("expected default applied with empty body, got %+v", exec.gotParams)
	}
}

func TestExecuteFunction_NotFound(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router, _ := setupFunctionExecuteRouter(repo, nil)
	w := doExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecuteFunction_NoExecutorReturns503WithCoercedParams(t *testing.T) {
	sig := `{"params":[{"name":"a","type":"integer","required":true}]}`
	repo := newExecuteFixtureRepo(sig)
	router, _ := setupFunctionExecuteRouter(repo, nil)

	w := doExecute(t, router, `{"parameters":{"a":42}}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d: %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	params, _ := body["parameters"].(map[string]interface{})
	if params["a"] != float64(42) {
		t.Errorf("expected coerced parameters echoed in 503 body, got %+v", body)
	}
}

func TestExecuteFunction_ExecutorErrorBecomes400(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{err: errors.New("boom")}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 from executor error, got %d: %s", w.Code, w.Body.String())
	}
}
