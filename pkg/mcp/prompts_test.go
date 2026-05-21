package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// OSV2-302 — prompts/list should be populated from OMS ActionType metadata
// (one prompt per ActionType across all ontologies), with declaration-order
// arguments and a working prompts/get handler. initialize must advertise the
// prompts capability too.

func newPromptsServer(t *testing.T) (*Server, *fakeOmsRepo) {
	t.Helper()
	srv, repo, _, _ := newTestServer(t)
	repo.ontologies = []oms.Ontology{
		{RID: "ri.weave.main.ontology.northwind", APIName: "northwind", DisplayName: "Northwind"},
		{RID: "ri.weave.main.ontology.chinook", APIName: "chinook", DisplayName: "Chinook"},
	}
	// Two action types on northwind, none on chinook — so we can verify the
	// handler enumerates ontologies and joins the per-ontology results.
	repo.actionTypes[repo.ontologies[0].RID] = []oms.ActionType{
		{
			RID:         "ri.weave.main.action-type.create-order",
			APIName:     "create-order",
			DisplayName: "Create Order",
			Description: "Place a new order for a customer.",
			Parameters: json.RawMessage(`[
				{"id":"customer","type":"string","required":true,"description":"Customer api name"},
				{"id":"note","type":"string","required":false,"description":"Optional note"}
			]`),
		},
		{
			RID:         "ri.weave.main.action-type.update-order",
			APIName:     "update-order",
			DisplayName: "Update Order",
			Description: "Mutate an existing order.",
			Parameters:  json.RawMessage(`[{"id":"orderId","type":"string","required":true}]`),
		},
	}
	return srv, repo
}

func TestPromptsList_Given_OMSWithActionTypes_When_Called_Then_ReturnsOnePromptPerActionType(t *testing.T) {
	srv, _ := newPromptsServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "prompts/list",
	})
	if resp.Error != nil {
		t.Fatalf("prompts/list error: %+v", resp.Error)
	}
	var body struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := remarshal(resp.Result, &body); err != nil {
		t.Fatalf("decode prompts: %v", err)
	}
	got := map[string]Prompt{}
	for _, p := range body.Prompts {
		got[p.Name] = p
	}
	if _, ok := got["northwind__create-order"]; !ok {
		t.Errorf("missing prompt northwind__create-order; got names=%v", names(body.Prompts))
	}
	if _, ok := got["northwind__update-order"]; !ok {
		t.Errorf("missing prompt northwind__update-order; got names=%v", names(body.Prompts))
	}
	p := got["northwind__create-order"]
	if p.Description == "" {
		t.Errorf("create-order prompt missing description")
	}
	if len(p.Arguments) == 0 {
		t.Errorf("create-order prompt missing arguments")
	}
}

func TestPromptsList_Given_DeclaredParameters_When_Called_Then_ArgumentsInOrderWithRequiredFlags(t *testing.T) {
	srv, _ := newPromptsServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "prompts/list",
	})
	var body struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := remarshal(resp.Result, &body); err != nil {
		t.Fatalf("decode prompts: %v", err)
	}
	var p Prompt
	for _, q := range body.Prompts {
		if q.Name == "northwind__create-order" {
			p = q
			break
		}
	}
	if len(p.Arguments) != 2 {
		t.Fatalf("create-order arguments len = %d, want 2", len(p.Arguments))
	}
	if p.Arguments[0].Name != "customer" || !p.Arguments[0].Required {
		t.Errorf("arg 0 = %+v, want name=customer required=true", p.Arguments[0])
	}
	if p.Arguments[1].Name != "note" || p.Arguments[1].Required {
		t.Errorf("arg 1 = %+v, want name=note required=false", p.Arguments[1])
	}
}

func TestPromptsGet_Given_ActionAndArguments_When_Called_Then_ReturnsRenderedUserMessage(t *testing.T) {
	srv, _ := newPromptsServer(t)
	params, _ := json.Marshal(map[string]any{
		"name":      "northwind__create-order",
		"arguments": map[string]any{"customer": "ALFKI"},
	})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: "prompts/get", Params: params,
	})
	if resp.Error != nil {
		t.Fatalf("prompts/get error: %+v", resp.Error)
	}
	var body struct {
		Description string          `json:"description"`
		Messages    []PromptMessage `json:"messages"`
	}
	if err := remarshal(resp.Result, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Messages) == 0 {
		t.Fatal("messages empty")
	}
	if body.Messages[0].Role != "user" {
		t.Errorf("role = %q, want user", body.Messages[0].Role)
	}
	if body.Messages[0].Content.Type != "text" {
		t.Errorf("content type = %q, want text", body.Messages[0].Content.Type)
	}
	text := body.Messages[0].Content.Text
	for _, must := range []string{"northwind", "create-order", "ALFKI"} {
		if !contains(text, must) {
			t.Errorf("rendered text missing %q; got: %s", must, text)
		}
	}
}

func TestPromptsList_Given_NilOMS_When_Called_Then_EmptyListNoPanic(t *testing.T) {
	srv := NewServer(nil, nil, nil)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`4`), Method: "prompts/list",
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	var body struct {
		Prompts []Prompt `json:"prompts"`
	}
	if err := remarshal(resp.Result, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Prompts) != 0 {
		t.Errorf("expected empty prompts, got %d", len(body.Prompts))
	}
}

func TestPromptsList_Given_OntologyListingFails_When_Called_Then_InternalError(t *testing.T) {
	srv, repo := newPromptsServer(t)
	repo.listOntologiesErr = errors.New("oms unavailable")

	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`6`), Method: "prompts/list",
	})
	if resp.Error == nil {
		t.Fatalf("expected prompts/list error")
	}
	if resp.Error.Code != CodeInternalError {
		t.Fatalf("error code = %d, want %d", resp.Error.Code, CodeInternalError)
	}
	for _, want := range []string{"list ontologies", "oms unavailable"} {
		if !contains(resp.Error.Message, want) {
			t.Fatalf("error message missing %q: %s", want, resp.Error.Message)
		}
	}
}

func TestPromptsList_Given_ActionTypeListingFails_When_Called_Then_InternalErrorNoPartialCatalogue(t *testing.T) {
	srv, repo := newPromptsServer(t)
	repo.listActionTypesErr = map[string]error{
		repo.ontologies[1].RID: errors.New("action metadata unavailable"),
	}

	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`7`), Method: "prompts/list",
	})
	if resp.Error == nil {
		t.Fatalf("expected prompts/list error")
	}
	if resp.Error.Code != CodeInternalError {
		t.Fatalf("error code = %d, want %d", resp.Error.Code, CodeInternalError)
	}
	for _, want := range []string{"list action types", "chinook", "action metadata unavailable"} {
		if !contains(resp.Error.Message, want) {
			t.Fatalf("error message missing %q: %s", want, resp.Error.Message)
		}
	}
}

func TestInitialize_Given_Server_When_HandshakeCalled_Then_AdvertisesPromptsCapability(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`8`), Method: "initialize",
	})
	var body struct {
		Capabilities map[string]json.RawMessage `json:"capabilities"`
	}
	if err := remarshal(resp.Result, &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body.Capabilities["prompts"]; !ok {
		t.Errorf("initialize capabilities missing prompts: %v", body.Capabilities)
	}
}

// remarshal converts an any-typed Response.Result back into bytes for
// strict-typed decoding in the tests. Cheaper than reflecting through the
// Response struct.
func remarshal(v any, into any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, into)
}

func names(ps []Prompt) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}
