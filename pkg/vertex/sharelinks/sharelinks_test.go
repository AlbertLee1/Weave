package sharelinks

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGenerateToken_Given_Default_When_Generate_Then_URLSafe(t *testing.T) {
	got, err := GenerateToken()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(got) < 32 {
		t.Fatalf("token too short: %d", len(got))
	}
	for _, c := range got {
		// URL-safe base64 alphabet = A-Z a-z 0-9 - _
		ok := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			t.Fatalf("non-URL-safe char in token: %q", c)
		}
	}
}

func TestGenerateToken_Given_TwoCalls_Then_Distinct(t *testing.T) {
	a, _ := GenerateToken()
	b, _ := GenerateToken()
	if a == b {
		t.Errorf("expected distinct tokens, both = %q", a)
	}
}

func TestEvaluateAccess_Given_RevokedLink_Then_Gone(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	link := Link{
		Revoked: true,
	}
	got := EvaluateAccess(link, AccessRequest{Now: now})
	if got.Decision != DecisionGone {
		t.Errorf("got %v, want Gone", got.Decision)
	}
}

func TestEvaluateAccess_Given_ExpiredLink_Then_Gone(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	link := Link{
		ExpiresAt: now.Add(-time.Minute),
	}
	got := EvaluateAccess(link, AccessRequest{Now: now})
	if got.Decision != DecisionGone {
		t.Errorf("got %v, want Gone", got.Decision)
	}
}

func TestEvaluateAccess_Given_SameOrg_Then_ReadOnly(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	link := Link{
		OwnerOrg:  "acme",
		ExpiresAt: now.Add(time.Hour),
	}
	got := EvaluateAccess(link, AccessRequest{Now: now, ViewerOrg: "acme"})
	if got.Decision != DecisionReadOnly {
		t.Errorf("got %v, want ReadOnly", got.Decision)
	}
}

func TestEvaluateAccess_Given_DifferentOrg_Then_Masked(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	link := Link{
		OwnerOrg:  "acme",
		ExpiresAt: now.Add(time.Hour),
	}
	got := EvaluateAccess(link, AccessRequest{Now: now, ViewerOrg: "other"})
	if got.Decision != DecisionMasked {
		t.Errorf("got %v, want Masked", got.Decision)
	}
}

func TestEvaluateAccess_Given_UnauthenticatedViewer_Then_Masked(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
	link := Link{OwnerOrg: "acme", ExpiresAt: now.Add(time.Hour)}
	got := EvaluateAccess(link, AccessRequest{Now: now, ViewerOrg: ""})
	if got.Decision != DecisionMasked {
		t.Errorf("got %v, want Masked", got.Decision)
	}
}

func TestMaskGraphPayload_Given_PropertyValues_Then_Redacted(t *testing.T) {
	graph := map[string]any{
		"name": "JFK Ops",
		"nodes": []map[string]any{
			{"id": "a", "properties": map[string]any{"capacity": 100, "label": "JFK"}},
			{"id": "b", "properties": map[string]any{"capacity": 200, "label": "LAX"}},
		},
	}
	got := MaskGraphPayload(graph)
	nodes, _ := got["nodes"].([]map[string]any)
	if nodes[0]["properties"].(map[string]any)["capacity"] != "•••" {
		t.Errorf("expected capacity masked, got %v", nodes[0]["properties"])
	}
	if nodes[0]["properties"].(map[string]any)["label"] != "•••" {
		t.Errorf("expected label masked, got %v", nodes[0]["properties"])
	}
	// Structural fields (id, name) remain.
	if got["name"] != "JFK Ops" {
		t.Errorf("expected name preserved")
	}
	if nodes[0]["id"] != "a" {
		t.Errorf("expected id preserved")
	}
}

func TestMaskGraphPayload_Given_JSONDecodedGraph_When_Masked_Then_RedactsPropertiesAndDoesNotMutateInput(t *testing.T) {
	var graph map[string]any
	if err := json.Unmarshal([]byte(`{
		"name": "JFK Ops",
		"nodes": [
			{"id": "a", "properties": {"capacity": 100, "label": "JFK"}},
			{"id": "b", "properties": {"capacity": 200, "label": "LAX"}}
		]
	}`), &graph); err != nil {
		t.Fatalf("unmarshal graph: %v", err)
	}

	got := MaskGraphPayload(graph)
	nodes, ok := got["nodes"].([]any)
	if !ok {
		t.Fatalf("masked nodes type = %T, want []any", got["nodes"])
	}
	first, ok := nodes[0].(map[string]any)
	if !ok {
		t.Fatalf("masked node type = %T, want map[string]any", nodes[0])
	}
	props := first["properties"].(map[string]any)
	if props["capacity"] != "•••" {
		t.Errorf("expected capacity masked, got %v", props["capacity"])
	}
	if props["label"] != "•••" {
		t.Errorf("expected label masked, got %v", props["label"])
	}
	if got["name"] != "JFK Ops" || first["id"] != "a" {
		t.Errorf("expected structural graph fields preserved, got graph=%v node=%v", got["name"], first["id"])
	}

	originalNodes := graph["nodes"].([]any)
	originalFirst := originalNodes[0].(map[string]any)
	originalProps := originalFirst["properties"].(map[string]any)
	if originalProps["capacity"] != float64(100) || originalProps["label"] != "JFK" {
		t.Errorf("original graph was mutated: %v", originalProps)
	}
}

func TestMaskGraphPayload_Given_GraphWithoutNodes_Then_NoChange(t *testing.T) {
	graph := map[string]any{"name": "empty"}
	got := MaskGraphPayload(graph)
	if got["name"] != "empty" {
		t.Errorf("expected name preserved")
	}
}
