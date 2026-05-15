package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/actions"
)

func TestHandleHealth_Given_AnyGet_When_Called_Then_Returns200OK(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	handleHealth(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("want 200 OK, got %d", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != "ok" {
		t.Errorf("want body %q, got %q", "ok", got)
	}
}

func TestHandleFunctions_Given_PostStub_When_Called_Then_ReturnsEmptyEditsResponse(t *testing.T) {
	req := actions.FunctionRequest{
		ActionTypeRID: "ri.action.main.actionType.test",
		ActionTypeAPI: "createCustomer",
		FunctionRID:   "ri.fn.main.function.create",
		Parameters:    map[string]interface{}{"name": "Acme"},
	}
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, "/functions/ri.fn.main.function.create", bytes.NewReader(body))
	w := httptest.NewRecorder()
	var log bytes.Buffer

	handleFunctions(&log)(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 OK, got %d (body=%q)", w.Code, w.Body.String())
	}
	var resp actions.FunctionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Edits) != 0 {
		t.Errorf("stub must return zero edits, got %d", len(resp.Edits))
	}
	if resp.Error != "" {
		t.Errorf("stub must not set error field, got %q", resp.Error)
	}
	if !strings.Contains(log.String(), "createCustomer") {
		t.Errorf("log must echo the action type api name for debugging, got %q", log.String())
	}
}

func TestHandleFunctions_Given_GetRequest_When_Called_Then_Returns405(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/functions/ri.fn.main.function.x", nil)
	w := httptest.NewRecorder()

	handleFunctions(new(bytes.Buffer))(w, r)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("want 405, got %d", w.Code)
	}
}

func TestHandleFunctions_Given_MalformedJSON_When_Posted_Then_Returns400(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/functions/ri.fn.main.function.x", strings.NewReader("{not json"))
	w := httptest.NewRecorder()

	handleFunctions(new(bytes.Buffer))(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d (body=%q)", w.Code, w.Body.String())
	}
}

func TestHandleFunctions_Given_BlankRid_When_Posted_Then_Returns400(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/functions/", strings.NewReader("{}"))
	w := httptest.NewRecorder()

	handleFunctions(new(bytes.Buffer))(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d (body=%q)", w.Code, w.Body.String())
	}
}

func TestDefaultAddr_Given_EnvUnset_When_Read_Then_Returns9000(t *testing.T) {
	t.Setenv("WEAVE_FUNCTION_RUNTIME_ADDR", "")
	if got := defaultAddr(); got != ":9000" {
		t.Errorf("want :9000, got %q", got)
	}
}

func TestDefaultAddr_Given_EnvSet_When_Read_Then_ReturnsEnvValue(t *testing.T) {
	t.Setenv("WEAVE_FUNCTION_RUNTIME_ADDR", "0.0.0.0:8765")
	if got := defaultAddr(); got != "0.0.0.0:8765" {
		t.Errorf("want 0.0.0.0:8765, got %q", got)
	}
}
