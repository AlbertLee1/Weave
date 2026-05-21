package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/oms"
)

func TestResourcesList_GivenPageSize_WhenListThenReturnsSortedPageAndCursor_P2A002(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	repo.objectTypes["ri.weave.main.ontology.demo"] = []oms.ObjectType{
		{RID: "ri.weave.main.objectType.order", APIName: "Order", DisplayName: "Order"},
		{RID: "ri.weave.main.objectType.customer", APIName: "Customer", DisplayName: "Customer"},
	}
	srv.SetObjectSetCatalog(&fakeObjectSetCatalog{
		entries: []ObjectSetEntry{
			{
				ID:         "tmp-a",
				Definition: map[string]any{"type": "base", "objectType": "Customer"},
				CreatedAt:  time.Date(2026, 5, 21, 9, 0, 0, 0, time.UTC),
			},
		},
	})

	first := callResourcesListP2A002(t, srv, map[string]any{"pageSize": 2})
	if len(first.Resources) != 2 {
		t.Fatalf("first page len = %d, want 2: %+v", len(first.Resources), first.Resources)
	}
	assertResourcesSortedP2A002(t, first.Resources)
	if first.NextCursor == "" {
		t.Fatalf("first page nextCursor empty, want continuation")
	}

	second := callResourcesListP2A002(t, srv, map[string]any{"cursor": first.NextCursor})
	if len(second.Resources) != 2 {
		t.Fatalf("second page len = %d, want remaining 2: %+v", len(second.Resources), second.Resources)
	}
	assertResourcesSortedP2A002(t, second.Resources)
	if second.NextCursor != "" {
		t.Fatalf("second page nextCursor = %q, want empty final page", second.NextCursor)
	}

	seen := map[string]bool{}
	for _, r := range append(first.Resources, second.Resources...) {
		if seen[r.URI] {
			t.Fatalf("duplicate resource URI across pages: %s", r.URI)
		}
		seen[r.URI] = true
	}

	full := callResourcesListP2A002(t, srv, map[string]any{})
	if len(full.Resources) != 4 {
		t.Fatalf("default full list len = %d, want 4: %+v", len(full.Resources), full.Resources)
	}
	if full.NextCursor != "" {
		t.Fatalf("default full list nextCursor = %q, want empty", full.NextCursor)
	}
}

func TestResourcesList_GivenMalformedCursor_WhenListThenInvalidParams_P2A002(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	params, err := json.Marshal(map[string]any{"cursor": "not-a-valid-resource-cursor"})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`77`),
		Method:  "resources/list",
		Params:  params,
	})
	if resp.Error == nil {
		t.Fatal("expected malformed cursor to fail")
	}
	if resp.Error.Code != CodeInvalidParams {
		t.Fatalf("error code = %d, want %d", resp.Error.Code, CodeInvalidParams)
	}
}

type resourcesListResultP2A002 struct {
	Resources  []Resource `json:"resources"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

func callResourcesListP2A002(t *testing.T, srv *Server, params map[string]any) resourcesListResultP2A002 {
	t.Helper()
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	resp := srv.Handle(context.Background(), &Request{
		JSONRPC: "2.0",
		ID:      json.RawMessage(`76`),
		Method:  "resources/list",
		Params:  paramsJSON,
	})
	if resp.Error != nil {
		t.Fatalf("resources/list error: %+v", resp.Error)
	}
	body, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var got resourcesListResultP2A002
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	return got
}

func assertResourcesSortedP2A002(t *testing.T, resources []Resource) {
	t.Helper()
	for i := 1; i < len(resources); i++ {
		if resources[i-1].URI > resources[i].URI {
			t.Fatalf("resources not URI-sorted: %+v", resources)
		}
	}
}
