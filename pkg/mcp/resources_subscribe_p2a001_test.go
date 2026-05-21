package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestResourcesSubscribe_GivenInitialize_WhenReadCapabilities_ThenSubscribeAdvertised_P2A001(t *testing.T) {
	srv, _, _, _ := newTestServer(t)

	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{}`),
	})
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}

	body, _ := json.Marshal(resp.Result)
	var got struct {
		Capabilities struct {
			Resources struct {
				Subscribe  bool `json:"subscribe"`
				ListChange bool `json:"listChanged"`
			} `json:"resources"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode initialize result: %v", err)
	}
	if !got.Capabilities.Resources.Subscribe {
		t.Fatalf("resources.subscribe = false, want true")
	}
	if got.Capabilities.Resources.ListChange {
		t.Fatalf("resources.listChanged = true, want false")
	}
}

func TestResourcesSubscribe_GivenKnownOntologyURI_WhenSubscribeAndUnsubscribe_ThenSuccess_P2A001(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	uri := "weave://ontology/ri.weave.main.ontology.demo"

	for _, method := range []string{"resources/subscribe", "resources/subscribe", "resources/unsubscribe", "resources/unsubscribe"} {
		resp := callResourceSubscriptionMethod(t, srv, method, uri)
		if resp.Error != nil {
			t.Fatalf("%s returned error: %+v", method, resp.Error)
		}
		body, _ := json.Marshal(resp.Result)
		if string(body) != "{}" {
			t.Fatalf("%s result = %s, want {}", method, body)
		}
	}
}

func TestResourcesSubscribe_GivenMalformedURI_WhenSubscribe_ThenInvalidParams_P2A001(t *testing.T) {
	srv, _, _, _ := newTestServer(t)

	resp := callResourceSubscriptionMethod(t, srv, "resources/subscribe", "https://example.invalid/demo")
	if resp.Error == nil {
		t.Fatal("expected malformed URI to fail")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Fatalf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
	if !strings.Contains(resp.Error.Message, "weave://") {
		t.Fatalf("error message = %q, want weave:// guidance", resp.Error.Message)
	}
}

func TestResourcesSubscribe_GivenUnknownResource_WhenSubscribe_ThenApplicationError_P2A001(t *testing.T) {
	srv, _, _, _ := newTestServer(t)

	resp := callResourceSubscriptionMethod(t, srv, "resources/subscribe", "weave://ontology/ri.weave.main.ontology.missing")
	if resp.Error == nil {
		t.Fatal("expected unknown resource to fail")
	}
	if resp.Error.Code != CodeToolError {
		t.Fatalf("error code = %d, want %d", resp.Error.Code, CodeToolError)
	}
	if !strings.Contains(strings.ToLower(resp.Error.Message), "not found") {
		t.Fatalf("error message = %q, want not found", resp.Error.Message)
	}
}

func callResourceSubscriptionMethod(t *testing.T, srv *Server, method, uri string) *Response {
	t.Helper()
	params, err := json.Marshal(map[string]any{"uri": uri})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`99`),
		Method:  method,
		Params:  params,
	})
}
