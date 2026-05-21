package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// OSV2-303 — weave-mcp must work as a transparent stdio↔HTTP MCP bridge:
// it reads JSON-RPC from stdin, POSTs each request to WEAVE_MCP_URL, and
// writes the verbatim JSON-RPC response back on stdout (one object per
// line). Local AI clients (Claude Desktop / Cursor) can then spawn
// weave-mcp and consume the full tools/resources/prompts surface served
// by cmd/server's /mcp HTTP transport — including the prompts added in
// OSV2-302 — without weave-mcp having to re-bootstrap PG/NATS.

func mustWriteJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

// stubServer returns a httptest.Server whose /mcp endpoint dispatches on
// the JSON-RPC method and yields the corresponding canned result.
func stubServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("decode upstream req: %v", err)
		}
		switch req.Method {
		case "initialize":
			mustWriteJSON(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"serverInfo":      map[string]any{"name": "weave-mcp", "version": "0.1.0"},
					"capabilities": map[string]any{
						"tools":     map[string]any{"listChanged": false},
						"prompts":   map[string]any{"listChanged": false},
						"resources": map[string]any{"listChanged": false, "subscribe": false},
					},
				},
			})
		case "tools/list":
			mustWriteJSON(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"tools": []any{
						map[string]any{"name": "weave_list_ontologies"},
						map[string]any{"name": "weave_apply_action"},
					},
				},
			})
		case "prompts/list":
			mustWriteJSON(t, w, map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"prompts": []any{
						map[string]any{
							"name":        "northwind__create-order",
							"description": "Place a new order for a customer.",
							"arguments": []any{
								map[string]any{"name": "customer", "required": true},
							},
						},
					},
				},
			})
		default:
			http.Error(w, "method not found", http.StatusNotImplemented)
		}
	}))
}

func TestHTTPBridge_Given_InitializeRequest_When_Run_Then_StdoutContainsUpstreamResponseWithPromptsCapability(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	if err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp"); err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	// Expect exactly one line of output.
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line of output, got %d: %q", len(lines), out.String())
	}
	var resp struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      json.Number    `json:"id"`
		Result  map[string]any `json:"result"`
		Error   any            `json:"error"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("decode response: %v\nline: %s", err, lines[0])
	}
	if resp.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", resp.JSONRPC)
	}
	if string(resp.ID) != "1" {
		t.Errorf("id = %v, want 1", resp.ID)
	}
	if resp.Error != nil {
		t.Errorf("unexpected error in bridged response: %v", resp.Error)
	}
	caps, ok := resp.Result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("missing capabilities: %v", resp.Result)
	}
	if _, ok := caps["prompts"]; !ok {
		t.Errorf("missing prompts capability in bridged initialize: %v", caps)
	}
}

func TestHTTPBridge_Given_ToolsListRequest_When_Run_Then_StdoutEchoesUpstreamTools(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}` + "\n")
	var out bytes.Buffer
	if err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp"); err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	var resp struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Result.Tools) != 2 {
		t.Fatalf("tools = %d, want 2: %+v", len(resp.Result.Tools), resp.Result.Tools)
	}
	want := map[string]bool{"weave_list_ontologies": true, "weave_apply_action": true}
	for _, tl := range resp.Result.Tools {
		if !want[tl.Name] {
			t.Errorf("unexpected tool name %q", tl.Name)
		}
	}
}

func TestHTTPBridge_Given_PromptsListRequest_When_Run_Then_StdoutEchoesUpstreamPrompts(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":2,"method":"prompts/list","params":{}}` + "\n")
	var out bytes.Buffer
	if err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp"); err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	var resp struct {
		Result struct {
			Prompts []struct {
				Name string `json:"name"`
			} `json:"prompts"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("decode: %v\nout=%s", err, out.String())
	}
	if len(resp.Result.Prompts) != 1 {
		t.Fatalf("prompts len = %d, want 1: %+v", len(resp.Result.Prompts), resp.Result.Prompts)
	}
	if resp.Result.Prompts[0].Name != "northwind__create-order" {
		t.Errorf("name = %q, want northwind__create-order", resp.Result.Prompts[0].Name)
	}
}

func TestBDD_HTTPBridge_Given_MixedJSONRPCBatch_When_Run_Then_StdoutEmitsOneBatchResponseLine(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var requests []struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(body, &requests); err != nil {
			t.Fatalf("decode upstream batch: %v; body=%s", err, body)
		}
		if len(requests) != 3 {
			t.Fatalf("upstream batch request count = %d, want 3", len(requests))
		}
		if requests[0].Method != "initialize" || requests[1].Method != "tools/list" || requests[2].Method != "notifications/initialized" {
			t.Fatalf("unexpected methods: %+v", requests)
		}
		if len(requests[0].ID) == 0 || len(requests[1].ID) == 0 {
			t.Fatalf("first two batch items should carry ids: %+v", requests)
		}
		if len(requests[2].ID) != 0 {
			t.Fatalf("notification item should not carry id: %+v", requests[2])
		}
		mustWriteJSON(t, w, []any{
			map[string]any{
				"jsonrpc": "2.0",
				"id":      requests[0].ID,
				"result":  map[string]any{"protocolVersion": "2024-11-05"},
			},
			map[string]any{
				"jsonrpc": "2.0",
				"id":      requests[1].ID,
				"result":  map[string]any{"tools": []any{}},
			},
		})
	}))
	defer srv.Close()

	in := bytes.NewBufferString(`[{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}},{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}},{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}]` + "\n")
	var out bytes.Buffer
	if err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp"); err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	line := strings.TrimSpace(out.String())
	if line == "" {
		t.Fatal("expected one JSON-RPC batch response line, got empty stdout")
	}
	if strings.Contains(line, "\n") {
		t.Fatalf("expected one response line, got multiple lines: %q", out.String())
	}
	var responses []struct {
		ID    json.RawMessage `json:"id"`
		Error any             `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &responses); err != nil {
		t.Fatalf("decode batch response: %v; line=%s", err, line)
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

func TestBDD_HTTPBridge_Given_InvalidItemInJSONRPCBatch_When_Run_Then_StdoutEmitsUpstreamBatchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var requests []map[string]any
		if err := json.Unmarshal(body, &requests); err != nil {
			t.Fatalf("decode upstream batch: %v; body=%s", err, body)
		}
		if len(requests) != 2 {
			t.Fatalf("upstream batch request count = %d, want 2", len(requests))
		}
		mustWriteJSON(t, w, []any{
			map[string]any{
				"jsonrpc": "2.0",
				"id":      nil,
				"error":   map[string]any{"code": -32600, "message": "method is required"},
			},
		})
	}))
	defer srv.Close()

	in := bytes.NewBufferString(`[{"jsonrpc":"2.0","method":"notifications/initialized","params":{}},{"jsonrpc":"2.0","params":{}}]` + "\n")
	var out bytes.Buffer
	if err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp"); err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	line := strings.TrimSpace(out.String())
	if line == "" {
		t.Fatal("expected upstream batch error line, got empty stdout")
	}
	var responses []struct {
		ID    json.RawMessage `json:"id"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &responses); err != nil {
		t.Fatalf("decode batch error response: %v; line=%s", err, line)
	}
	if len(responses) != 1 {
		t.Fatalf("response count = %d, want 1: %+v", len(responses), responses)
	}
	if string(responses[0].ID) != "null" {
		t.Fatalf("error id = %s, want null", responses[0].ID)
	}
	if responses[0].Error == nil || responses[0].Error.Code != -32600 {
		t.Fatalf("error = %+v, want code -32600", responses[0].Error)
	}
}

func TestBDD_HTTPBridge_Given_NotificationOnlyJSONRPCBatch_When_Run_Then_StdoutStaysEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var requests []struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(body, &requests); err != nil {
			t.Fatalf("decode upstream batch: %v; body=%s", err, body)
		}
		if len(requests) != 2 {
			t.Fatalf("upstream batch request count = %d, want 2", len(requests))
		}
		for i, req := range requests {
			if req.Method != "notifications/initialized" {
				t.Fatalf("request %d method = %q, want notifications/initialized", i, req.Method)
			}
			if len(req.ID) != 0 {
				t.Fatalf("request %d should not carry id: %+v", i, req)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	in := bytes.NewBufferString(`[{"jsonrpc":"2.0","method":"notifications/initialized","params":{}},{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}]` + "\n")
	var out bytes.Buffer
	if err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp"); err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected notification-only batch to produce no stdout, got %q", out.String())
	}
}

func TestHTTPBridge_Given_UpstreamUnreachable_When_Run_Then_StdoutEmitsJSONRPCError(t *testing.T) {
	// Point at a port nothing should be listening on. 127.0.0.1:1 is the
	// canonical "definitely closed" address.
	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":99,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	err := RunHTTPBridge(context.Background(), in, &out, "http://127.0.0.1:1/mcp")
	if err != nil {
		t.Fatalf("RunHTTPBridge should not surface upstream errors to caller, got %v", err)
	}
	line := strings.TrimSpace(out.String())
	if line == "" {
		t.Fatal("expected one JSON-RPC error line, got empty stdout")
	}
	var resp struct {
		ID    json.Number `json:"id"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("decode: %v\nline=%s", err, line)
	}
	if resp.Error == nil {
		t.Fatalf("expected non-nil error in response, got line=%s", line)
	}
	if string(resp.ID) != "99" {
		t.Errorf("id = %v, want 99 (echoed from request)", resp.ID)
	}
}
