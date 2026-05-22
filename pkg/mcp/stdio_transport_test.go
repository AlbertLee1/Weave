package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestStdioTransport_DispatchesAndWritesResponse(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	transport := NewStdioTransport(srv, in, &out)
	if err := transport.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Output should be one JSON line.
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response line, got %d: %q", len(lines), out.String())
	}
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != nil {
		t.Errorf("error = %+v", resp.Error)
	}
}

func TestStdioTransport_NotificationProducesNoOutput(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` + "\n")
	var out bytes.Buffer
	transport := NewStdioTransport(srv, in, &out)
	if err := transport.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output for notification, got %q", out.String())
	}
}

func TestStdioTransport_InvalidJSON_WritesParseError(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	in := strings.NewReader("{not json\n")
	var out bytes.Buffer
	transport := NewStdioTransport(srv, in, &out)
	if err := transport.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var resp Response
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != CodeParseError {
		t.Errorf("error = %+v, want code %d", resp.Error, CodeParseError)
	}
}

func TestBDD_StdioTransport_GivenInvalidIDShape_WhenRun_ThenInvalidRequestWithNullID_P2A002(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	in := strings.NewReader(`{"jsonrpc":"2.0","id":[1],"method":"initialize","params":{}}` + "\n")
	var out bytes.Buffer
	transport := NewStdioTransport(srv, in, &out)
	if err := transport.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response line, got %d: %q", len(lines), out.String())
	}
	var resp Response
	if err := json.Unmarshal([]byte(lines[0]), &resp); err != nil {
		t.Fatalf("decode: %v; line=%q", err, lines[0])
	}
	if resp.Error == nil || resp.Error.Code != CodeInvalidRequest {
		t.Fatalf("error = %+v, want code %d", resp.Error, CodeInvalidRequest)
	}
	if string(resp.ID) != "null" {
		t.Fatalf("id = %s, want null", resp.ID)
	}
}

func TestStdioTransport_MultiLineSession(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n",
	)
	var out bytes.Buffer
	transport := NewStdioTransport(srv, in, &out)
	if err := transport.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}

func TestBDD_StdioTransportProcessesJSONRPCBatchOnOneLine(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	in := strings.NewReader(`[{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}},{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}},{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}]` + "\n")
	var out bytes.Buffer
	transport := NewStdioTransport(srv, in, &out)
	if err := transport.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 response line, got %d: %q", len(lines), out.String())
	}
	var responses []Response
	if err := json.Unmarshal([]byte(lines[0]), &responses); err != nil {
		t.Fatalf("decode batch response: %v; line=%q", err, lines[0])
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
