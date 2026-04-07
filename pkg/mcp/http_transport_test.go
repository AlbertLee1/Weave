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
