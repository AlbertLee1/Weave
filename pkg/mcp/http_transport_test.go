package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPTransport_DecodesJSONRPC(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %s", resp.JSONRPC)
	}
	if resp.Error != nil {
		t.Errorf("error = %+v", resp.Error)
	}
}

func TestHTTPTransport_ReturnsValidResponse(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)

	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      42,
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	buf, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(buf))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %s", got)
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// id round-trips.
	var id int
	if err := json.Unmarshal(resp.ID, &id); err != nil {
		t.Errorf("id decode: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
}

func TestBDD_HTTPTransportProcessesJSONRPCBatch(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)

	body := strings.NewReader(`[
		{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}},
		{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}},
		{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}
	]`)
	req := httptest.NewRequest(http.MethodPost, "/mcp", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var responses []Response
	if err := json.NewDecoder(rr.Body).Decode(&responses); err != nil {
		t.Fatalf("decode batch response: %v; body=%q", err, rr.Body.String())
	}
	if len(responses) != 2 {
		t.Fatalf("response count = %d, want 2: %+v", len(responses), responses)
	}
	for i, resp := range responses {
		if resp.Error != nil {
			t.Fatalf("response %d error = %+v", i, resp.Error)
		}
		var id int
		if err := json.Unmarshal(resp.ID, &id); err != nil {
			t.Fatalf("response %d id decode: %v", i, err)
		}
		if want := i + 1; id != want {
			t.Fatalf("response %d id = %d, want %d", i, id, want)
		}
	}
}

func TestBDD_HTTPTransportRejectsEmptyJSONRPCBatch(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`[]`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidRequest {
		t.Fatalf("error = %+v, want code %d", resp.Error, CodeInvalidRequest)
	}
}

func TestBDD_HTTPTransport_GivenInvalidIDShape_WhenPost_ThenInvalidRequestWithNullID_P2A002(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":{"nested":1},"method":"initialize","params":{}}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rr.Code, rr.Body.String())
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rr.Body.String())
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidRequest {
		t.Fatalf("error = %+v, want code %d", resp.Error, CodeInvalidRequest)
	}
	if string(resp.ID) != "null" {
		t.Fatalf("id = %s, want null", resp.ID)
	}
}

func TestHTTPTransport_InvalidJSON_Returns32700_HTTPStatus200(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{not valid`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// JSON-RPC 2.0 mandates that protocol-level errors are returned as a
	// well-formed envelope with an error object, NOT via HTTP status codes.
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (errors travel in JSON-RPC envelope)", rr.Code)
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeParseError {
		t.Errorf("error = %+v, want code %d", resp.Error, CodeParseError)
	}
}

func TestBDD_HTTPTransport_GivenValidNonObjectJSON_WhenPost_ThenInvalidRequest_P2A002(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`"not-a-request"`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rr.Code, rr.Body.String())
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rr.Body.String())
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidRequest {
		t.Fatalf("error = %+v, want code %d", resp.Error, CodeInvalidRequest)
	}
	if string(resp.ID) != "null" {
		t.Fatalf("id = %s, want null", resp.ID)
	}
}

func TestBDD_HTTPTransport_GivenBatchWithNonObjectMember_WhenPost_ThenInvalidRequestForMember_P2A002(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`[
		{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}},
		42
	]`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rr.Code, rr.Body.String())
	}
	var responses []Response
	if err := json.NewDecoder(rr.Body).Decode(&responses); err != nil {
		t.Fatalf("decode batch response: %v; body=%q", err, rr.Body.String())
	}
	if len(responses) != 2 {
		t.Fatalf("response count = %d, want 2: %+v", len(responses), responses)
	}
	if responses[0].Error != nil {
		t.Fatalf("valid sibling error = %+v", responses[0].Error)
	}
	if responses[1].Error == nil || responses[1].Error.Code != CodeInvalidRequest {
		t.Fatalf("invalid member error = %+v, want code %d", responses[1].Error, CodeInvalidRequest)
	}
	if string(responses[1].ID) != "null" {
		t.Fatalf("invalid member id = %s, want null", responses[1].ID)
	}
}

func TestBDD_HTTPTransport_GivenOversizedBody_WhenPost_ThenJSONRPCError_P2A003(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(strings.Repeat("x", maxBodySize+1)))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (oversized body errors travel in JSON-RPC envelope); body=%q", rr.Code, rr.Body.String())
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rr.Body.String())
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidRequest {
		t.Fatalf("error = %+v, want code %d", resp.Error, CodeInvalidRequest)
	}
	if string(resp.ID) != "null" {
		t.Fatalf("id = %s, want null", resp.ID)
	}
	if !strings.Contains(resp.Error.Message, "request body exceeds") {
		t.Fatalf("error message = %q, want oversized body reason", resp.Error.Message)
	}
}

func TestBDD_HTTPTransportResourcesListRejectsUnsafePageSizeCursor_RLLM001(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)
	cursor, err := encodeResourceListCursor("weave://ontology/ri.weave.main.ontology.demo", 1)
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      91,
		"method":  "resources/list",
		"params": map[string]any{
			"cursor":   cursor,
			"pageSize": maxIntRLLM001(),
		},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%q", rr.Code, rr.Body.String())
	}
	var resp Response
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v; body=%q", err, rr.Body.String())
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidParams {
		t.Fatalf("error = %+v, want invalid params", resp.Error)
	}
}

func TestHTTPTransport_NonPostReturns405(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)
	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rr.Code)
	}
}

func TestHTTPTransport_NotificationReturnsNoBody(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	handler := NewHTTPHandler(srv)
	// Notifications have no id and per JSON-RPC 2.0 must NOT receive a response.
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204 for notification", rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("body should be empty, got %q", rr.Body.String())
	}
}
