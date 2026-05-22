package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

// OSV2-307 — resources/list must include every ObjectType under each
// ontology so an MCP client (Claude Desktop / Cursor) can resource://read
// a single type's schema instead of having to fetch the whole ontology
// snapshot and client-side filter.

func TestResourcesList_Given_OntologyWithObjectTypes_When_List_Then_ObjectTypeURIsIncluded_US307(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	repo.objectTypes["ri.weave.main.ontology.demo"] = []oms.ObjectType{
		{RID: "ri.weave.main.objectType.order", APIName: "Order", DisplayName: "Order",
			Description: "Customer order"},
		{RID: "ri.weave.main.objectType.customer", APIName: "Customer", DisplayName: "Customer",
			Description: "A buyer"},
	}

	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "resources/list",
		Params: json.RawMessage(`{}`),
	})
	if resp.Error != nil {
		t.Fatalf("resources/list error: %+v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	var got struct {
		Resources []Resource `json:"resources"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := map[string]bool{
		"weave://objecttype/demo/Order":    false,
		"weave://objecttype/demo/Customer": false,
	}
	hasOntology := false
	for _, r := range got.Resources {
		if r.URI == "weave://ontology/ri.weave.main.ontology.demo" {
			hasOntology = true
		}
		if _, ok := want[r.URI]; ok {
			want[r.URI] = true
			if r.MimeType != "application/json" {
				t.Errorf("MimeType for %q = %q, want application/json", r.URI, r.MimeType)
			}
			if r.Name == "" {
				t.Errorf("Name empty for %q", r.URI)
			}
		}
	}
	if !hasOntology {
		t.Errorf("ontology resource still missing: %+v", got.Resources)
	}
	for uri, ok := range want {
		if !ok {
			t.Errorf("expected objecttype URI %q missing", uri)
		}
	}

	// Stable sort: every URI is in ascending order.
	for i := 1; i < len(got.Resources); i++ {
		if got.Resources[i-1].URI > got.Resources[i].URI {
			t.Errorf("resources not URI-sorted: %v", got.Resources)
			break
		}
	}
}

func TestResourcesRead_Given_ObjectTypeURI_When_Read_Then_SchemaTextHasApiName_US307(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	repo.objectTypes["ri.weave.main.ontology.demo"] = []oms.ObjectType{
		{RID: "ri.weave.main.objectType.order", APIName: "Order", DisplayName: "Order",
			PrimaryKey: "orderId"},
	}

	params, _ := json.Marshal(map[string]any{"uri": "weave://objecttype/demo/Order"})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "resources/read",
		Params: params,
	})
	if resp.Error != nil {
		t.Fatalf("resources/read error: %+v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	var got struct {
		Contents []ResourceContent `json:"contents"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Contents) != 1 {
		t.Fatalf("len(contents) = %d, want 1", len(got.Contents))
	}
	c := got.Contents[0]
	if c.URI != "weave://objecttype/demo/Order" {
		t.Errorf("URI = %q", c.URI)
	}
	if c.MimeType != "application/json" {
		t.Errorf("MimeType = %q", c.MimeType)
	}
	if !strings.Contains(c.Text, `"Order"`) {
		t.Errorf("schema missing Order: %s", c.Text)
	}
	if !strings.Contains(c.Text, `"orderId"`) {
		t.Errorf("schema missing primaryKey orderId: %s", c.Text)
	}
}

func TestResourcesRead_Given_UnknownObjectType_When_Read_Then_ToolError_US307(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	params, _ := json.Marshal(map[string]any{"uri": "weave://objecttype/demo/Ghost"})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: "resources/read",
		Params: params,
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown object type")
	}
	if resp.Error.Code != CodeToolError {
		t.Errorf("Code = %d, want %d (CodeToolError)", resp.Error.Code, CodeToolError)
	}
	if !strings.Contains(strings.ToLower(resp.Error.Message), "object type") {
		t.Errorf("error.message should mention 'object type', got %q", resp.Error.Message)
	}
}

func TestResourcesRead_Given_UnknownOntologyOnObjectTypeURI_When_Read_Then_ToolError_US307(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	params, _ := json.Marshal(map[string]any{"uri": "weave://objecttype/nosuch/Order"})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`4`), Method: "resources/read",
		Params: params,
	})
	if resp.Error == nil {
		t.Fatal("expected error for unknown ontology")
	}
	if resp.Error.Code != CodeToolError {
		t.Errorf("Code = %d, want %d (CodeToolError)", resp.Error.Code, CodeToolError)
	}
	if !strings.Contains(strings.ToLower(resp.Error.Message), "ontology") {
		t.Errorf("error.message should mention ontology lookup, got %q", resp.Error.Message)
	}
}

func TestResourcesRead_Given_ObjectTypeURIMissingType_When_Read_Then_InvalidParams_US307(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	// 'weave://objecttype/demo' — missing the type segment.
	params, _ := json.Marshal(map[string]any{"uri": "weave://objecttype/demo"})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`5`), Method: "resources/read",
		Params: params,
	})
	if resp.Error == nil {
		t.Fatal("expected error for malformed URI")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("Code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

// erroringRepo is a slim fake that always returns an error from
// ListObjectTypes — used to assert resources/list propagates the failure
// instead of silently dropping the rest of the catalogue.
type erroringRepo struct {
	oms.Repository
	ont oms.Ontology
}

func (e *erroringRepo) ListOntologies(ctx context.Context) ([]oms.Ontology, error) {
	return []oms.Ontology{e.ont}, nil
}

func (e *erroringRepo) GetOntology(ctx context.Context, rid string) (*oms.Ontology, error) {
	if rid == e.ont.RID || rid == e.ont.APIName {
		o := e.ont
		return &o, nil
	}
	return nil, oms.ErrNotFound
}

func (e *erroringRepo) ListObjectTypes(ctx context.Context, ontologyRID string) ([]oms.ObjectType, error) {
	return nil, errors.New("boom")
}

func TestResourcesList_Given_ListObjectTypesErrors_When_List_Then_InternalError_US307(t *testing.T) {
	repo := &erroringRepo{ont: oms.Ontology{RID: "ri.weave.main.ontology.demo", APIName: "demo", DisplayName: "Demo"}}
	srv := NewServer(nil, repo, nil)

	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`6`), Method: "resources/list",
		Params: json.RawMessage(`{}`),
	})
	if resp.Error == nil {
		t.Fatal("expected error when ListObjectTypes fails")
	}
	if resp.Error.Code != CodeInternalError {
		t.Errorf("Code = %d, want %d (CodeInternalError)", resp.Error.Code, CodeInternalError)
	}
}
