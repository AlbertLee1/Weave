package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/oms"
)

func TestResourcesRead_GivenObjectTypeResource_WhenReadThenReturnsSchemaBundle_P2A001(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	const (
		ontologyRID   = "ri.weave.main.ontology.demo"
		objectTypeRID = "ri.weave.main.objectType.order"
	)
	repo.objectTypes[ontologyRID] = []oms.ObjectType{
		{RID: objectTypeRID, APIName: "Order", DisplayName: "Order"},
	}
	repo.properties[objectTypeRID] = []oms.Property{
		{RID: "ri.weave.main.property.order-id", ObjectTypeRID: objectTypeRID, APIName: "id", BaseType: "string", IsSearchable: true},
		{RID: "ri.weave.main.property.customer-id", ObjectTypeRID: objectTypeRID, APIName: "customerId", BaseType: "string"},
	}
	repo.outgoingLinkTypes[objectTypeRID] = []oms.LinkType{
		{RID: "ri.weave.main.linkType.order-customer", APIName: "orderCustomer", DisplayName: "Order Customer", SourceObjectType: "Order", TargetObjectType: "Customer", Cardinality: "MANY_TO_ONE"},
	}

	content := readResourceContentP2A001(t, srv, "weave://objecttype/demo/Order")
	var got struct {
		ObjectType        oms.ObjectType `json:"objectType"`
		Properties        []oms.Property `json:"properties"`
		OutgoingLinkTypes []oms.LinkType `json:"outgoingLinkTypes"`
	}
	if err := json.Unmarshal([]byte(content.Text), &got); err != nil {
		t.Fatalf("decode objecttype resource text: %v\n%s", err, content.Text)
	}
	if got.ObjectType.APIName != "Order" {
		t.Fatalf("objectType.apiName = %q, want Order; content=%s", got.ObjectType.APIName, content.Text)
	}
	if len(got.Properties) != 2 {
		t.Fatalf("properties len = %d, want 2; content=%s", len(got.Properties), content.Text)
	}
	if got.Properties[0].APIName != "id" || !got.Properties[0].IsSearchable {
		t.Fatalf("first property = %+v, want searchable id", got.Properties[0])
	}
	if len(got.OutgoingLinkTypes) != 1 || got.OutgoingLinkTypes[0].APIName != "orderCustomer" {
		t.Fatalf("outgoingLinkTypes = %+v, want orderCustomer", got.OutgoingLinkTypes)
	}
}

func TestResourcesRead_GivenOntologyResource_WhenReadThenObjectTypesIncludeProperties_P2A001(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	const (
		ontologyRID   = "ri.weave.main.ontology.demo"
		objectTypeRID = "ri.weave.main.objectType.customer"
	)
	repo.objectTypes[ontologyRID] = []oms.ObjectType{
		{RID: objectTypeRID, APIName: "Customer", DisplayName: "Customer"},
	}
	repo.properties[objectTypeRID] = []oms.Property{
		{RID: "ri.weave.main.property.customer-name", ObjectTypeRID: objectTypeRID, APIName: "name", BaseType: "string"},
	}

	content := readResourceContentP2A001(t, srv, "weave://ontology/"+ontologyRID)
	var got struct {
		ObjectTypes []oms.ObjectType `json:"objectTypes"`
	}
	if err := json.Unmarshal([]byte(content.Text), &got); err != nil {
		t.Fatalf("decode ontology resource text: %v\n%s", err, content.Text)
	}
	if len(got.ObjectTypes) != 1 {
		t.Fatalf("objectTypes len = %d, want 1; content=%s", len(got.ObjectTypes), content.Text)
	}
	if len(got.ObjectTypes[0].Properties) != 1 || got.ObjectTypes[0].Properties[0].APIName != "name" {
		t.Fatalf("objectTypes[0].properties = %+v, want name property; content=%s",
			got.ObjectTypes[0].Properties, content.Text)
	}
}

func TestResourcesRead_GivenPropertyLookupFails_WhenReadThenReturnsToolError_P2A001(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	const (
		ontologyRID   = "ri.weave.main.ontology.demo"
		objectTypeRID = "ri.weave.main.objectType.order"
	)
	repo.objectTypes[ontologyRID] = []oms.ObjectType{
		{RID: objectTypeRID, APIName: "Order", DisplayName: "Order"},
	}
	repo.listPropertiesErr = map[string]error{objectTypeRID: errors.New("metadata store unavailable")}

	params, err := json.Marshal(map[string]any{"uri": "weave://objecttype/demo/Order"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`501`),
		Method:  "resources/read",
		Params:  params,
	})
	if resp.Error == nil {
		t.Fatalf("expected resources/read to fail when ListProperties fails")
	}
	if resp.Error.Code != CodeToolError {
		t.Fatalf("error code = %d, want %d", resp.Error.Code, CodeToolError)
	}
}

func readResourceContentP2A001(t *testing.T, srv *Server, uri string) ResourceContent {
	t.Helper()
	params, err := json.Marshal(map[string]any{"uri": uri})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`500`),
		Method:  "resources/read",
		Params:  params,
	})
	if resp.Error != nil {
		t.Fatalf("resources/read error: %+v", resp.Error)
	}
	body, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var got struct {
		Contents []ResourceContent `json:"contents"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if len(got.Contents) != 1 {
		t.Fatalf("contents len = %d, want 1", len(got.Contents))
	}
	return got.Contents[0]
}
