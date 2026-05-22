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

func TestBDD_HTTPBridge_Given_ResponseRequiredRequestAndEmptyUpstreamBody_When_Run_Then_StdoutEmitsJSONRPCError_P2A003(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{name: "HTTP 200", status: http.StatusOK},
		{name: "HTTP 204", status: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestBodies := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				requestBodies <- string(body)
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			in := bytes.NewBufferString(`{"jsonrpc":"2.0","id":"empty-31","method":"initialize","params":{}}` + "\n")
			var out bytes.Buffer
			err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp")

			if err != nil {
				t.Fatalf("RunHTTPBridge: %v", err)
			}
			select {
			case body := <-requestBodies:
				if !strings.Contains(body, `"method":"initialize"`) {
					t.Fatalf("upstream body = %s, want initialize request", body)
				}
			default:
				t.Fatal("upstream did not receive request")
			}

			line := strings.TrimSpace(out.String())
			if line == "" {
				t.Fatal("expected one JSON-RPC error line, got empty stdout")
			}
			if strings.Contains(line, "\n") {
				t.Fatalf("expected one response line, got multiple lines: %q", out.String())
			}
			var resp struct {
				ID    json.RawMessage `json:"id"`
				Error *struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				t.Fatalf("decode JSON-RPC empty-body error: %v\nout=%s", err, out.String())
			}
			if string(resp.ID) != `"empty-31"` {
				t.Fatalf("id = %s, want original request id", resp.ID)
			}
			if resp.Error == nil || resp.Error.Code != -32000 {
				t.Fatalf("error = %+v, want JSON-RPC server error -32000", resp.Error)
			}
			if !strings.Contains(strings.ToLower(resp.Error.Message), "empty") {
				t.Fatalf("error message %q should explain empty upstream response", resp.Error.Message)
			}
		})
	}
}

func TestBDD_HTTPBridge_Given_NotificationOnlyRequestAndEmptyUpstreamBody_When_Run_Then_StdoutStaysEmpty_P2A003(t *testing.T) {
	requestBodies := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requestBodies <- string(body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	in := bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` + "\n")
	var out bytes.Buffer
	err := RunHTTPBridge(context.Background(), in, &out, srv.URL+"/mcp")

	if err != nil {
		t.Fatalf("RunHTTPBridge: %v", err)
	}
	select {
	case body := <-requestBodies:
		if !strings.Contains(body, `"method":"notifications/initialized"`) {
			t.Fatalf("upstream body = %s, want notification request", body)
		}
	default:
		t.Fatal("upstream did not receive request")
	}
	if out.Len() != 0 {
		t.Fatalf("expected notification-only request to produce no stdout, got %q", out.String())
	}
}
