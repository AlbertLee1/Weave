package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// OSV2-305 — weave-mcp HTTP bridge must forward Authorization (Bearer
// token) or X-Weave-API-Key headers when the operator sets the matching
// env var. Without this, cmd/server running with AUTH_MODE=token rejects
// every bridge request with 401 and Claude Desktop / Cursor cannot
// connect. Token wins when both env vars are set; both unset keeps the
// historical "no auth" behaviour intact (OSV2-303 contract).

// recordingServer is a httptest.Server that records every request's
// Authorization + X-Weave-API-Key header. The route honours custom
// auth predicates so each test case can demand a specific header.
type recordingServer struct {
	mu       sync.Mutex
	requests []http.Header
	srv      *httptest.Server
}

func newRecordingServer(t *testing.T, accept func(r *http.Request) (status int, body string)) *recordingServer {
	t.Helper()
	rs := &recordingServer{}
	rs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.requests = append(rs.requests, r.Header.Clone())
		rs.mu.Unlock()
		_, _ = io.ReadAll(r.Body) // drain
		status, body := accept(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(rs.srv.Close)
	return rs
}

func (rs *recordingServer) Requests() []http.Header {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	out := make([]http.Header, len(rs.requests))
	copy(out, rs.requests)
	return out
}

// canned 200 initialize response that ignores the body and echoes id=1.
const okInit = `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}`

func TestHTTPBridge_Given_WEAVE_MCP_TOKEN_Set_When_Run_Then_AuthorizationHeaderForwarded_US305(t *testing.T) {
	srv := newRecordingServer(t, func(r *http.Request) (int, string) {
		if r.Header.Get("Authorization") == "Bearer test-token-abc" {
			return 200, okInit
		}
		return 401, `{"error":"unauthorized"}`
	})

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	err := RunHTTPBridge(context.Background(), in, &out, srv.srv.URL+"/mcp",
		WithBearerToken("test-token-abc"))
	if err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	line := strings.TrimSpace(out.String())
	var resp struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		t.Fatalf("decode: %v\nline=%s", err, line)
	}
	if resp.Error != nil {
		t.Errorf("expected success path, got error: %v\nline=%s", resp.Error, line)
	}
	if resp.Result == nil || resp.Result["protocolVersion"] != "2024-11-05" {
		t.Errorf("missing 200 protocolVersion result: %v", resp.Result)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 || reqs[0].Get("Authorization") != "Bearer test-token-abc" {
		t.Fatalf("Authorization header not forwarded; got %q", reqs[0].Get("Authorization"))
	}
}

func TestHTTPBridge_Given_NoAuthEnv_When_Run_Then_NoAuthHeaderSent_And_401ProducesJSONRPCError_US305(t *testing.T) {
	srv := newRecordingServer(t, func(r *http.Request) (int, string) {
		if r.Header.Get("Authorization") == "" && r.Header.Get("X-Weave-API-Key") == "" {
			return 401, `{"error":"unauthorized"}`
		}
		return 200, okInit
	})

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	err := RunHTTPBridge(context.Background(), in, &out, srv.srv.URL+"/mcp")
	if err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	reqs := srv.Requests()
	if len(reqs) != 1 {
		t.Fatalf("len(requests) = %d, want 1", len(reqs))
	}
	if got := reqs[0].Get("Authorization"); got != "" {
		t.Errorf("Authorization unexpectedly set: %q", got)
	}
	if got := reqs[0].Get("X-Weave-API-Key"); got != "" {
		t.Errorf("X-Weave-API-Key unexpectedly set: %q", got)
	}

	// 401 from upstream -> JSON-RPC error code -32000 with 'upstream' / 'status' in message
	var resp struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("decode: %v\nout=%s", err, out.String())
	}
	if resp.Error == nil {
		t.Fatalf("expected JSON-RPC error line, got: %s", out.String())
	}
	if resp.Error.Code != -32000 {
		t.Errorf("error.code = %d, want -32000", resp.Error.Code)
	}
	low := strings.ToLower(resp.Error.Message)
	if !strings.Contains(low, "upstream") && !strings.Contains(low, "status") {
		t.Errorf("error.message lacks transport hint: %q", resp.Error.Message)
	}
}

func TestHTTPBridge_Given_APIKeyOnly_When_Run_Then_XWeaveAPIKeyHeaderForwarded_US305(t *testing.T) {
	srv := newRecordingServer(t, func(r *http.Request) (int, string) {
		if r.Header.Get("X-Weave-API-Key") == "wvk_unit_test" {
			return 200, okInit
		}
		return 401, `{"error":"unauthorized"}`
	})

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	err := RunHTTPBridge(context.Background(), in, &out, srv.srv.URL+"/mcp",
		WithAPIKey("wvk_unit_test"))
	if err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	reqs := srv.Requests()
	if got := reqs[0].Get("X-Weave-API-Key"); got != "wvk_unit_test" {
		t.Errorf("X-Weave-API-Key = %q, want 'wvk_unit_test'", got)
	}
	if got := reqs[0].Get("Authorization"); got != "" {
		t.Errorf("Authorization unexpectedly set: %q", got)
	}
}

func TestHTTPBridge_Given_BothTokenAndAPIKey_When_Run_Then_OnlyAuthorizationSent_US305(t *testing.T) {
	srv := newRecordingServer(t, func(r *http.Request) (int, string) {
		// Always 200 — we only care about which header was actually sent.
		return 200, okInit
	})

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	err := RunHTTPBridge(context.Background(), in, &out, srv.srv.URL+"/mcp",
		WithBearerToken("tok"),
		WithAPIKey("key"),
	)
	if err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	reqs := srv.Requests()
	if got := reqs[0].Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q, want 'Bearer tok'", got)
	}
	if got := reqs[0].Get("X-Weave-API-Key"); got != "" {
		t.Errorf("X-Weave-API-Key should be empty when token wins, got %q", got)
	}
}

func TestHTTPBridge_Given_MultipleRequests_When_Run_Then_AuthHeaderSetOnEveryCall_US305(t *testing.T) {
	srv := newRecordingServer(t, func(r *http.Request) (int, string) {
		// Echo back the id so we know which response is which.
		var sniff struct {
			ID json.RawMessage `json:"id"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &sniff)
		return 200, `{"jsonrpc":"2.0","id":` + string(sniff.ID) + `,"result":{}}`
	})

	in := bytes.NewBufferString(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"prompts/list","params":{}}`,
	}, "\n") + "\n")
	var out bytes.Buffer
	err := RunHTTPBridge(context.Background(), in, &out, srv.srv.URL+"/mcp",
		WithBearerToken("persistent-tok"))
	if err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("len(requests) = %d, want 3", len(reqs))
	}
	for i, h := range reqs {
		if got := h.Get("Authorization"); got != "Bearer persistent-tok" {
			t.Errorf("request[%d] Authorization = %q, want 'Bearer persistent-tok'", i, got)
		}
	}
}
