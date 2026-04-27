package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

// fakeObjectSetCatalog is an in-memory ObjectSetCatalog for the resources
// tests. It mirrors the shape pkg/oss/objectset.Store exposes via its
// ListEntries / GetEntry helpers so the production wiring stays a thin pass-
// through.
type fakeObjectSetCatalog struct {
	entries []ObjectSetEntry
}

func (f *fakeObjectSetCatalog) ListObjectSets() []ObjectSetEntry {
	out := make([]ObjectSetEntry, len(f.entries))
	copy(out, f.entries)
	return out
}

func (f *fakeObjectSetCatalog) GetObjectSet(id string) (*ObjectSetEntry, error) {
	for i := range f.entries {
		if f.entries[i].ID == id {
			e := f.entries[i]
			return &e, nil
		}
	}
	return nil, ErrObjectSetNotFound
}

func TestServer_Initialize_AdvertisesResourcesCapability(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: "initialize",
		Params: json.RawMessage(`{}`),
	})
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	var result map[string]any
	_ = json.Unmarshal(body, &result)
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("capabilities missing")
	}
	if _, ok := caps["resources"].(map[string]any); !ok {
		t.Errorf("resources capability not advertised: %#v", caps)
	}
}

func TestServer_ResourcesList_IncludesOntologies(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "resources/list",
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
	if len(got.Resources) == 0 {
		t.Fatal("expected at least one ontology resource, got 0")
	}
	found := false
	for _, r := range got.Resources {
		if r.URI == "weave://ontology/ri.weave.main.ontology.demo" {
			found = true
			if r.MimeType != "application/json" {
				t.Errorf("MimeType = %q, want application/json", r.MimeType)
			}
			if r.Name == "" {
				t.Errorf("Name should be non-empty")
			}
		}
	}
	if !found {
		t.Errorf("expected ontology resource; got %+v", got.Resources)
	}
}

func TestServer_ResourcesList_IncludesObjectSets(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	srv.SetObjectSetCatalog(&fakeObjectSetCatalog{
		entries: []ObjectSetEntry{
			{
				ID:         "abc-123",
				Definition: map[string]any{"type": "base", "objectType": "User"},
				CreatedAt:  time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
			},
		},
	})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: "resources/list",
		Params: json.RawMessage(`{}`),
	})
	if resp.Error != nil {
		t.Fatalf("resources/list error: %+v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	var got struct {
		Resources []Resource `json:"resources"`
	}
	_ = json.Unmarshal(body, &got)
	found := false
	for _, r := range got.Resources {
		if r.URI == "weave://objectset/abc-123" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected objectset resource weave://objectset/abc-123; got %+v", got.Resources)
	}
}

func TestServer_ResourcesRead_Ontology_ReturnsSchema(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	repo.objectTypes["ri.weave.main.ontology.demo"] = []oms.ObjectType{
		{RID: "ri.weave.main.objectType.user", APIName: "User", DisplayName: "User"},
	}
	repo.actionTypes["ri.weave.main.ontology.demo"] = []oms.ActionType{
		{RID: "ri.weave.main.actionType.create", APIName: "createUser", DisplayName: "Create User"},
	}
	params, _ := json.Marshal(map[string]any{"uri": "weave://ontology/ri.weave.main.ontology.demo"})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`4`), Method: "resources/read",
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
	if c.URI != "weave://ontology/ri.weave.main.ontology.demo" {
		t.Errorf("URI = %q", c.URI)
	}
	if c.MimeType != "application/json" {
		t.Errorf("MimeType = %q", c.MimeType)
	}
	if !contains(c.Text, `"User"`) {
		t.Errorf("schema missing User: %s", c.Text)
	}
	if !contains(c.Text, `"createUser"`) {
		t.Errorf("schema missing createUser: %s", c.Text)
	}
}

func TestServer_ResourcesRead_ObjectSet_ReturnsDefinition(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	srv.SetObjectSetCatalog(&fakeObjectSetCatalog{
		entries: []ObjectSetEntry{
			{
				ID:         "abc-123",
				Definition: map[string]any{"type": "base", "objectType": "User"},
				CreatedAt:  time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC),
			},
		},
	})
	params, _ := json.Marshal(map[string]any{"uri": "weave://objectset/abc-123"})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`5`), Method: "resources/read",
		Params: params,
	})
	if resp.Error != nil {
		t.Fatalf("resources/read error: %+v", resp.Error)
	}
	body, _ := json.Marshal(resp.Result)
	var got struct {
		Contents []ResourceContent `json:"contents"`
	}
	_ = json.Unmarshal(body, &got)
	if len(got.Contents) != 1 {
		t.Fatalf("len(contents) = %d, want 1", len(got.Contents))
	}
	if !contains(got.Contents[0].Text, `"objectType": "User"`) {
		t.Errorf("objectset definition missing objectType: %s", got.Contents[0].Text)
	}
}

func TestServer_ResourcesRead_UnknownOntology_Returns_NotFound(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	params, _ := json.Marshal(map[string]any{"uri": "weave://ontology/ri.weave.main.ontology.missing"})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`6`), Method: "resources/read",
		Params: params,
	})
	if resp.Error == nil {
		t.Fatalf("expected error")
	}
	// MCP convention: missing resources → application error
	if resp.Error.Code != CodeToolError {
		t.Errorf("Code = %d, want %d (CodeToolError)", resp.Error.Code, CodeToolError)
	}
}

func TestServer_ResourcesRead_UnknownScheme_Returns_InvalidParams(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	params, _ := json.Marshal(map[string]any{"uri": "weave://unknown/foo"})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`7`), Method: "resources/read",
		Params: params,
	})
	if resp.Error == nil {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("Code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestServer_ResourcesRead_MissingURI_Returns_InvalidParams(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`8`), Method: "resources/read",
		Params: json.RawMessage(`{}`),
	})
	if resp.Error == nil {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Errorf("Code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

func TestServer_ResourcesRead_NoCatalog_ObjectSetURI_NotFound(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	// no SetObjectSetCatalog
	params, _ := json.Marshal(map[string]any{"uri": "weave://objectset/foo"})
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0", ID: json.RawMessage(`9`), Method: "resources/read",
		Params: params,
	})
	if resp.Error == nil {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != CodeToolError {
		t.Errorf("Code = %d, want %d", resp.Error.Code, CodeToolError)
	}
}
