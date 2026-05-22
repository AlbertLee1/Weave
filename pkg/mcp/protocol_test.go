package mcp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestProtocol_ParseValidRequest(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("ParseRequest returned error: %v", err)
	}
	if req.JSONRPC != "2.0" {
		t.Errorf("JSONRPC = %q, want 2.0", req.JSONRPC)
	}
	if req.Method != "tools/list" {
		t.Errorf("Method = %q, want tools/list", req.Method)
	}
	if req.ID == nil {
		t.Errorf("ID = nil, want non-nil for non-notification")
	}
}

func TestProtocol_GivenInvalidIDShape_WhenParseRequest_ThenInvalidRequest_P2A002(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "object", raw: `{"jsonrpc":"2.0","id":{"nested":1},"method":"tools/list","params":{}}`},
		{name: "array", raw: `{"jsonrpc":"2.0","id":[1],"method":"tools/list","params":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequest([]byte(tc.raw))
			if err == nil {
				t.Fatal("ParseRequest returned nil error for invalid id shape")
			}
			rpcErr, ok := err.(*RPCError)
			if !ok {
				t.Fatalf("error = %T, want *RPCError", err)
			}
			if rpcErr.Code != CodeInvalidRequest {
				t.Fatalf("code = %d, want %d", rpcErr.Code, CodeInvalidRequest)
			}
		})
	}
}

func TestProtocol_GivenValidIDShapes_WhenParseRequest_ThenAccepted_P2A002(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
	}{
		{name: "string", id: `"abc"`},
		{name: "number", id: `123`},
		{name: "null", id: `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := ParseRequest([]byte(`{"jsonrpc":"2.0","id":` + tc.id + `,"method":"tools/list","params":{}}`))
			if err != nil {
				t.Fatalf("ParseRequest returned error: %v", err)
			}
			if string(req.ID) != tc.id {
				t.Fatalf("id = %s, want %s", req.ID, tc.id)
			}
			if req.IsNotification() {
				t.Fatal("request with explicit id should not be treated as notification")
			}
		})
	}
}

func TestProtocol_ParseInvalidJSON_Returns32700(t *testing.T) {
	raw := []byte(`{not valid json`)
	_, err := ParseRequest(raw)
	if err == nil {
		t.Fatalf("expected error from ParseRequest")
	}
	rpcErr, ok := err.(*RPCError)
	if !ok {
		t.Fatalf("expected *RPCError, got %T", err)
	}
	if rpcErr.Code != CodeParseError {
		t.Errorf("Code = %d, want %d", rpcErr.Code, CodeParseError)
	}
}

func TestProtocol_GivenValidJSONNonObject_WhenParseRequest_ThenInvalidRequest_P2A002(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{name: "string", raw: `"not-a-request"`},
		{name: "number", raw: `42`},
		{name: "boolean", raw: `true`},
		{name: "array", raw: `[{"jsonrpc":"2.0","id":1,"method":"tools/list"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequest([]byte(tc.raw))
			if err == nil {
				t.Fatal("ParseRequest returned nil error for non-object JSON")
			}
			rpcErr, ok := err.(*RPCError)
			if !ok {
				t.Fatalf("error = %T, want *RPCError", err)
			}
			if rpcErr.Code != CodeInvalidRequest {
				t.Fatalf("code = %d, want %d", rpcErr.Code, CodeInvalidRequest)
			}
		})
	}
}

func TestProtocol_ErrorResponseFormat(t *testing.T) {
	resp := NewErrorResponse(json.RawMessage(`42`), CodeMethodNotFound, "method not found", nil)
	buf, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", decoded["jsonrpc"])
	}
	errObj, ok := decoded["error"].(map[string]any)
	if !ok {
		t.Fatalf("error field missing or wrong type")
	}
	if int(errObj["code"].(float64)) != CodeMethodNotFound {
		t.Errorf("error.code = %v, want %d", errObj["code"], CodeMethodNotFound)
	}
	if errObj["message"] != "method not found" {
		t.Errorf("error.message = %v", errObj["message"])
	}
	// Result must NOT be set on an error response.
	if _, hasResult := decoded["result"]; hasResult {
		t.Errorf("error response should not have a result field")
	}
}

func TestProtocol_NotificationHasNoID(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`)
	req, err := ParseRequest(raw)
	if err != nil {
		t.Fatalf("ParseRequest: %v", err)
	}
	if !req.IsNotification() {
		t.Errorf("IsNotification = false, want true for missing id")
	}
	if req.ID != nil {
		t.Errorf("ID = %v, want nil for notification", req.ID)
	}
}

func TestProtocol_SuccessResponseFormat(t *testing.T) {
	resp := NewSuccessResponse(json.RawMessage(`"abc"`), map[string]any{"ok": true})
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(resp); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v", decoded["jsonrpc"])
	}
	if _, hasErr := decoded["error"]; hasErr {
		t.Errorf("success response should not have error field")
	}
	result, ok := decoded["result"].(map[string]any)
	if !ok {
		t.Fatalf("result missing")
	}
	if result["ok"] != true {
		t.Errorf("result.ok = %v", result["ok"])
	}
}
