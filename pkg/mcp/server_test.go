package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestServer_Initialize(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	})
	if resp == nil {
		t.Fatal("Handle returned nil for initialize")
	}
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	body, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result["protocolVersion"] != ProtocolVersion {
		t.Errorf("protocolVersion = %v, want %s", result["protocolVersion"], ProtocolVersion)
	}
	info, ok := result["serverInfo"].(map[string]any)
	if !ok {
		t.Fatalf("serverInfo missing or wrong type")
	}
	if info["name"] != ServerName {
		t.Errorf("serverInfo.name = %v, want %s", info["name"], ServerName)
	}
}

func TestServer_ToolsList_ReturnsRegistered(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`2`),
		Method:  "tools/list",
		Params:  json.RawMessage(`{}`),
	})
	if resp.Error != nil {
		t.Fatalf("tools/list error: %+v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	var got struct {
		Tools []ToolDefinition `json:"tools"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Tools) != 7 {
		t.Errorf("len(tools) = %d, want 7", len(got.Tools))
	}
	wantNames := map[string]bool{
		"weave_list_ontologies":   true,
		"weave_list_object_types": true,
		"weave_get_object":        true,
		"weave_list_objects":      true,
		"weave_search_objects":    true,
		"weave_list_action_types": true,
		"weave_apply_action":      true,
	}
	for _, td := range got.Tools {
		if !wantNames[td.Name] {
			t.Errorf("unexpected tool %q", td.Name)
		}
		delete(wantNames, td.Name)
	}
	for n := range wantNames {
		t.Errorf("missing tool %q", n)
	}
}

func TestServer_ToolsCall_Success(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	params := map[string]any{
		"name":      "weave_list_ontologies",
		"arguments": map[string]any{},
	}
	paramsJSON, _ := json.Marshal(params)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`3`),
		Method:  "tools/call",
		Params:  paramsJSON,
	})
	if resp.Error != nil {
		t.Fatalf("tools/call error: %+v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	var result ToolResult
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Content) == 0 {
		t.Errorf("empty content")
	}
	if result.IsError {
		t.Errorf("IsError = true")
	}
}

func TestServer_ToolsCall_UnknownTool_Returns32601(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	params := map[string]any{
		"name":      "weave_does_not_exist",
		"arguments": map[string]any{},
	}
	paramsJSON, _ := json.Marshal(params)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`4`),
		Method:  "tools/call",
		Params:  paramsJSON,
	})
	if resp.Error == nil {
		t.Fatalf("expected error response")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("Code = %d, want %d", resp.Error.Code, CodeMethodNotFound)
	}
}

func TestServer_ToolsCall_InvalidParams_Returns32602(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	params := map[string]any{
		"name":      "weave_list_object_types",
		"arguments": map[string]any{}, // missing required "ontology"
	}
	paramsJSON, _ := json.Marshal(params)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`5`),
		Method:  "tools/call",
		Params:  paramsJSON,
	})
	if resp.Error == nil {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("Code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestServer_UnknownMethod_Returns32601(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`6`),
		Method:  "no/such",
		Params:  json.RawMessage(`{}`),
	})
	if resp.Error == nil || resp.Error.Code != CodeMethodNotFound {
		t.Errorf("got %+v, want method-not-found", resp.Error)
	}
}

func TestServer_PromptsList_EmptyOK(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`7`),
		Method:  "prompts/list",
		Params:  json.RawMessage(`{}`),
	})
	if resp.Error != nil {
		t.Errorf("error = %+v", resp.Error)
	}
}

func TestServer_ResourcesList_EmptyOK(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`8`),
		Method:  "resources/list",
		Params:  json.RawMessage(`{}`),
	})
	if resp.Error != nil {
		t.Errorf("error = %+v", resp.Error)
	}
}
