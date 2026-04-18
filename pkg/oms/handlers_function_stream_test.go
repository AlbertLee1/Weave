package oms_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/functions/fnerrors"
	"github.com/liyang/weave/pkg/oms"
)

// US-219: POST /functions/{rid}/execute?stream=1 returns NDJSON.
//
// Format: each newline-delimited JSON object is either
//   {"item": <value>}        — one element of the streamed result
//   {"error": {"code","reason"}}  — terminal error mid-stream
//
// Pre-execution failures (validation, 404, quota) still return regular HTTP
// errors with a single JSON body — the NDJSON contract only kicks in after
// the executor has been dispatched.

func doStreamExecute(t *testing.T, router *chi.Mux, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions/add/execute?stream=1", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func parseNDJSON(t *testing.T, body string) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 0, 4096), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("bad NDJSON line %q: %v", string(line), err)
		}
		out = append(out, m)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	return out
}

func TestExecuteFunction_StreamArrayResult_EmitsOneItemPerLine(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{result: []interface{}{
		map[string]interface{}{"id": "a"},
		map[string]interface{}{"id": "b"},
		map[string]interface{}{"id": "c"},
	}}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doStreamExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/x-ndjson" {
		t.Errorf("expected Content-Type=application/x-ndjson, got %q", ct)
	}
	lines := parseNDJSON(t, w.Body.String())
	if len(lines) != 3 {
		t.Fatalf("expected 3 NDJSON lines, got %d: %q", len(lines), w.Body.String())
	}
	for i, want := range []string{"a", "b", "c"} {
		item, ok := lines[i]["item"].(map[string]interface{})
		if !ok {
			t.Fatalf("line %d not an item object: %+v", i, lines[i])
		}
		if item["id"] != want {
			t.Errorf("line %d: expected id=%s, got %+v", i, want, item)
		}
	}
}

func TestExecuteFunction_StreamScalarResult_EmitsSingleLine(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{result: float64(42)}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doStreamExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	lines := parseNDJSON(t, w.Body.String())
	if len(lines) != 1 {
		t.Fatalf("expected 1 NDJSON line, got %d: %q", len(lines), w.Body.String())
	}
	if lines[0]["item"] != float64(42) {
		t.Errorf("expected item=42, got %+v", lines[0])
	}
}

func TestExecuteFunction_StreamEmptyArray_EmitsNoLines(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{result: []interface{}{}}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doStreamExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != "" {
		t.Errorf("expected empty body for empty array, got %q", got)
	}
}

func TestExecuteFunction_StreamExecutorError_EmitsErrorLine(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{err: errors.New("boom")}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doStreamExecute(t, router, `{"parameters":{}}`)
	// Once the stream has been opened the status code is fixed at 200; the
	// error is delivered in-band as the terminal NDJSON line so the SDK
	// iterator can surface it without parsing HTTP status codes.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (in-band error), got %d: %s", w.Code, w.Body.String())
	}
	lines := parseNDJSON(t, w.Body.String())
	if len(lines) != 1 {
		t.Fatalf("expected 1 NDJSON line, got %d: %q", len(lines), w.Body.String())
	}
	errObj, ok := lines[0]["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %+v", lines[0])
	}
	if errObj["reason"] != "boom" {
		t.Errorf("expected reason=boom, got %+v", errObj)
	}
	if errObj["code"] != "FunctionExecutionFailed" {
		t.Errorf("expected code=FunctionExecutionFailed, got %+v", errObj)
	}
}

func TestExecuteFunction_StreamTimeoutError_EmitsTimeoutLine(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{err: fnerrorsWrap(fnerrors.ErrTimeout, "ran too long")}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doStreamExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	lines := parseNDJSON(t, w.Body.String())
	if len(lines) != 1 {
		t.Fatalf("expected 1 NDJSON line, got %d: %q", len(lines), w.Body.String())
	}
	errObj, ok := lines[0]["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %+v", lines[0])
	}
	if errObj["code"] != "FunctionExecutionTimeout" {
		t.Errorf("expected code=FunctionExecutionTimeout, got %+v", errObj)
	}
}

func TestExecuteFunction_StreamMemoryError_EmitsMemoryLine(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &stubFunctionExecutor{err: fnerrorsWrap(fnerrors.ErrMemoryLimit, "heap exploded")}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doStreamExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	lines := parseNDJSON(t, w.Body.String())
	if len(lines) != 1 {
		t.Fatalf("expected 1 NDJSON line, got %d: %q", len(lines), w.Body.String())
	}
	errObj, ok := lines[0]["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected error object, got %+v", lines[0])
	}
	if errObj["code"] != "FunctionMemoryLimitExceeded" {
		t.Errorf("expected code=FunctionMemoryLimitExceeded, got %+v", errObj)
	}
}

func TestExecuteFunction_StreamValidationError_StillReturns400(t *testing.T) {
	// Pre-execution validation failures should still be a regular 400 with
	// a single JSON body — not in-band NDJSON. The stream contract only
	// kicks in after the executor is dispatched.
	sig := `{"params":[{"name":"a","type":"integer","required":true}]}`
	repo := newExecuteFixtureRepo(sig)
	exec := &stubFunctionExecutor{result: "ok"}
	router, _ := setupFunctionExecuteRouter(repo, exec)

	w := doStreamExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing-required, got %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct == "application/x-ndjson" {
		t.Errorf("validation errors must not use NDJSON Content-Type, got %q", ct)
	}
}

func TestExecuteFunction_StreamFunctionNotFound_StillReturns404(t *testing.T) {
	repo := &mockRepo{
		ontologies: []oms.Ontology{{
			RID:         "ri.ontology.main.ontology.o1",
			APIName:     "northwind",
			DisplayName: "Northwind",
		}},
	}
	router, _ := setupFunctionExecuteRouter(repo, nil)

	w := doStreamExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecuteFunction_StreamNoExecutor_StillReturns503(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	router, _ := setupFunctionExecuteRouter(repo, nil)

	w := doStreamExecute(t, router, `{"parameters":{}}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for no-executor wired, got %d: %s", w.Code, w.Body.String())
	}
}

func TestExecuteFunction_StreamRespectsContextDeadline(t *testing.T) {
	repo := newExecuteFixtureRepo(`{}`)
	exec := &slowExecutor{delay: 10 * 1000 * 1000 * 1000} // 10s in nanoseconds
	handler := oms.NewOMSHandler(repo)
	handler.SetFunctionExecutor(exec)

	r := chi.NewRouter()
	r.Post("/api/v2/ontologies/{ontologyApiName}/functions/{functionRid}/execute", handler.ExecuteFunction)

	req := httptest.NewRequest(http.MethodPost, "/api/v2/ontologies/northwind/functions/add/execute?stream=1", nil)
	ctx, cancel := context.WithTimeout(req.Context(), 100*1000*1000) // 100ms
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 with in-band error line, got %d: %s", w.Code, w.Body.String())
	}
	lines := parseNDJSON(t, w.Body.String())
	if len(lines) != 1 {
		t.Fatalf("expected 1 NDJSON error line, got %d: %q", len(lines), w.Body.String())
	}
	errObj, _ := lines[0]["error"].(map[string]interface{})
	if errObj == nil || errObj["code"] != "FunctionExecutionTimeout" {
		t.Errorf("expected code=FunctionExecutionTimeout from ctx deadline, got %+v", lines[0])
	}
}
