package mcp

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
)

// fakeSemanticSearcher implements SemanticSearcher for the AI tool tests.
type fakeSemanticSearcher struct {
	hits []SemanticHit
	err  error
	last SemanticSearchRequest
}

func (f *fakeSemanticSearcher) SemanticSearch(_ context.Context, req SemanticSearchRequest) (*SemanticSearchResult, error) {
	f.last = req
	if f.err != nil {
		return nil, f.err
	}
	return &SemanticSearchResult{Hits: f.hits, Model: "fake-model"}, nil
}

// --- semantic_search ---

func TestTool_SemanticSearch_HappyPath(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	searcher := &fakeSemanticSearcher{
		hits: []SemanticHit{
			{PrimaryKey: "u1", Distance: 0.10},
			{PrimaryKey: "u2", Distance: 0.25},
		},
	}
	srv.SetSemanticSearcher(searcher)

	res, err := srv.Registry().Call(context.Background(), "weave_semantic_search", map[string]any{
		"ontology":   "demo",
		"objectType": "User",
		"query":      "engineers in seattle",
		"k":          float64(5),
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !contains(res.Content[0].Text, "u1") || !contains(res.Content[0].Text, "u2") {
		t.Errorf("text missing PKs: %s", res.Content[0].Text)
	}
	if searcher.last.Ontology != "demo" || searcher.last.ObjectType != "User" {
		t.Errorf("searcher.last = %+v", searcher.last)
	}
	if searcher.last.K != 5 {
		t.Errorf("K = %d, want 5", searcher.last.K)
	}
	if searcher.last.QueryText != "engineers in seattle" {
		t.Errorf("QueryText = %q", searcher.last.QueryText)
	}
}

func TestTool_SemanticSearch_NotConfigured(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	_, err := srv.Registry().Call(context.Background(), "weave_semantic_search", map[string]any{
		"ontology":   "demo",
		"objectType": "User",
		"query":      "anything",
	})
	if err == nil {
		t.Fatal("expected error when semantic searcher is not configured")
	}
}

// --- ask_objectset ---

func TestTool_AskObjectSet_HydratesObjects(t *testing.T) {
	srv, _, svc, _ := newTestServer(t)
	searcher := &fakeSemanticSearcher{
		hits: []SemanticHit{
			{PrimaryKey: "u1", Distance: 0.10},
		},
	}
	srv.SetSemanticSearcher(searcher)
	svc.getResult = oss.FormatObject("User", "u1", map[string]any{"name": "Alice"})

	res, err := srv.Registry().Call(context.Background(), "weave_ask_objectset", map[string]any{
		"ontology":   "demo",
		"objectType": "User",
		"question":   "who joined recently",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !contains(res.Content[0].Text, "Alice") {
		t.Errorf("expected hydrated object in result: %s", res.Content[0].Text)
	}
	if searcher.last.QueryText != "who joined recently" {
		t.Errorf("query = %q", searcher.last.QueryText)
	}
}

// --- explain_object ---

func TestTool_ExplainObject_BundlesMetadataAndData(t *testing.T) {
	srv, repo, svc, _ := newTestServer(t)
	repo.objectTypes["demo"] = []oms.ObjectType{
		{
			RID:         "ri.weave.main.objectType.user",
			APIName:     "User",
			DisplayName: "User",
			Description: "An end user account",
		},
	}
	svc.getResult = oss.FormatObject("User", "u1", map[string]any{"name": "Alice", "email": "alice@example.com"})

	res, err := srv.Registry().Call(context.Background(), "weave_explain_object", map[string]any{
		"ontology":   "demo",
		"objectType": "User",
		"primaryKey": "u1",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	text := res.Content[0].Text
	// Must include the live object data...
	if !contains(text, "Alice") {
		t.Errorf("missing object data in explanation: %s", text)
	}
	// ...and the type-level description.
	if !contains(text, "An end user account") {
		t.Errorf("missing type description in explanation: %s", text)
	}
}

func TestTool_ExplainObject_UnknownType(t *testing.T) {
	srv, _, svc, _ := newTestServer(t)
	svc.getErr = oms.ErrNotFound
	_, err := srv.Registry().Call(context.Background(), "weave_explain_object", map[string]any{
		"ontology":   "demo",
		"objectType": "User",
		"primaryKey": "u1",
	})
	if err == nil {
		t.Fatal("expected error when object cannot be found")
	}
}

// --- draft_action ---

func TestTool_DraftAction_RendersTemplate(t *testing.T) {
	srv, repo, _, _ := newTestServer(t)
	repo.actionTypes["demo"] = []oms.ActionType{
		{
			RID:         "ri.weave.main.actionType.create-user",
			APIName:     "createUser",
			DisplayName: "Create User",
			Description: "Creates a new end user account",
			// Stored parameter array — see ParseParameterDefs in pkg/actions.
			Parameters: []byte(`[
				{"id":"name","type":"string","required":true,"description":"Display name"},
				{"id":"age","type":"integer","required":false,"description":"Age in years"}
			]`),
		},
	}

	res, err := srv.Registry().Call(context.Background(), "weave_draft_action", map[string]any{
		"ontology":   "demo",
		"actionType": "createUser",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	text := res.Content[0].Text
	if !contains(text, "createUser") {
		t.Errorf("missing actionType apiName: %s", text)
	}
	if !contains(text, "name") || !contains(text, "age") {
		t.Errorf("missing parameter ids: %s", text)
	}
	if !contains(text, "Display name") {
		t.Errorf("missing parameter description: %s", text)
	}
}

func TestTool_DraftAction_UnknownAction(t *testing.T) {
	srv, _, _, _ := newTestServer(t)
	_, err := srv.Registry().Call(context.Background(), "weave_draft_action", map[string]any{
		"ontology":   "demo",
		"actionType": "doesNotExist",
	})
	if err == nil {
		t.Fatal("expected error for unknown action type")
	}
}
