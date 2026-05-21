package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBDD_HTTPBridge_Given_StalledEndpointAndConfiguredTimeout_When_Run_Then_StdoutEmitsJSONRPCErrorWithinTimeout_P2A003(t *testing.T) {
	unblock := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-unblock
	}))
	t.Cleanup(func() {
		close(unblock)
		srv.Close()
	})

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":23,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer

	started := time.Now()
	err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp", WithHTTPTimeout(25*time.Millisecond))
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("RunHTTPBridge should convert upstream timeout to JSON-RPC error, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("RunHTTPBridge elapsed %s, want bounded by configured timeout", elapsed)
	}

	var resp struct {
		ID    json.Number `json:"id"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("decode JSON-RPC timeout response: %v\nout=%s", err, out.String())
	}
	if string(resp.ID) != "23" {
		t.Fatalf("id = %s, want 23", resp.ID)
	}
	if resp.Error == nil || resp.Error.Code != -32000 {
		t.Fatalf("error = %+v, want JSON-RPC server error -32000", resp.Error)
	}
	msg := strings.ToLower(resp.Error.Message)
	if !strings.Contains(msg, "timeout") && !strings.Contains(msg, "deadline") {
		t.Fatalf("timeout error message %q should mention timeout/deadline", resp.Error.Message)
	}
}

func TestBDD_HTTPBridge_Given_WEAVE_MCP_HTTP_TIMEOUT_When_EnvParsed_Then_ConfiguredTimeoutUsedAndAuthPrecedenceKept_P2A003(t *testing.T) {
	t.Setenv("WEAVE_MCP_HTTP_TIMEOUT", "100ms")
	t.Setenv("WEAVE_MCP_TOKEN", "token-wins")
	t.Setenv("WEAVE_MCP_API_KEY", "wvk_should_not_send")

	requestHeaders := make(chan http.Header, 1)
	unblock := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestHeaders <- r.Header.Clone()
		<-unblock
	}))
	t.Cleanup(func() {
		close(unblock)
		srv.Close()
	})

	opts, err := bridgeOptionsFromEnv()
	if err != nil {
		t.Fatalf("bridgeOptionsFromEnv: %v", err)
	}

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":24,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	started := time.Now()
	if err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp", opts...); err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("RunHTTPBridge elapsed %s, want WEAVE_MCP_HTTP_TIMEOUT to bound the request", elapsed)
	}

	select {
	case h := <-requestHeaders:
		if got := h.Get("Authorization"); got != "Bearer token-wins" {
			t.Fatalf("Authorization = %q, want bearer token from env", got)
		}
		if got := h.Get("X-Weave-API-Key"); got != "" {
			t.Fatalf("X-Weave-API-Key = %q, want empty because token wins", got)
		}
	default:
		t.Fatal("upstream did not receive request headers")
	}
}

func TestBDD_HTTPBridge_Given_Invalid_WEAVE_MCP_HTTP_TIMEOUT_When_EnvParsed_Then_ErrorNamesEnvVar_P2A003(t *testing.T) {
	t.Setenv("WEAVE_MCP_HTTP_TIMEOUT", "definitely-not-a-duration")

	_, err := bridgeOptionsFromEnv()
	if err == nil {
		t.Fatal("bridgeOptionsFromEnv returned nil error for invalid WEAVE_MCP_HTTP_TIMEOUT")
	}
	if !strings.Contains(err.Error(), "WEAVE_MCP_HTTP_TIMEOUT") {
		t.Fatalf("error %q should name WEAVE_MCP_HTTP_TIMEOUT", err.Error())
	}
}
