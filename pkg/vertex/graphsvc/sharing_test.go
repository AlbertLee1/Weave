package graphsvc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestMaskLayerPropertyValues_Given_PayloadWithProps_When_Masked_Then_ValuesReplaced
//
// Verifies that maskLayerPropertyValues replaces every scalar leaf under any
// layer's `properties` subtree with "***", and preserves keys + non-layer
// fields.
func TestMaskLayerPropertyValues_Given_PayloadWithProps_When_Masked_Then_ValuesReplaced(t *testing.T) {
	in := json.RawMessage(`{
		"layers": [
			{
				"objectType": "Airport",
				"objects": [
					{"objectRid": "rid1", "properties": {"name": "JFK", "code": "JFK", "elev": 13}},
					{"objectRid": "rid2", "properties": {"name": "LAX"}}
				]
			}
		],
		"edges": [{"linkTypeRid": "lt1", "source": "rid1", "target": "rid2"}],
		"positions": {"rid1": {"x": 1.0, "y": 2.0}}
	}`)
	out := maskLayerPropertyValues(in)

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode masked payload: %v", err)
	}
	layers := got["layers"].([]any)
	objects := layers[0].(map[string]any)["objects"].([]any)
	props := objects[0].(map[string]any)["properties"].(map[string]any)
	for k, v := range props {
		if v != "***" {
			t.Errorf("property %q = %v, want \"***\"", k, v)
		}
	}
	if _, ok := props["name"]; !ok {
		t.Error("property key \"name\" should remain visible")
	}
	if objects[0].(map[string]any)["objectRid"] != "rid1" {
		t.Errorf("non-property field objectRid was masked: %v", objects[0])
	}
	// Edges and positions are NOT under "properties" — must survive untouched.
	edges := got["edges"].([]any)
	if edges[0].(map[string]any)["source"] != "rid1" {
		t.Errorf("edge source masked: %v", edges[0])
	}
	positions := got["positions"].(map[string]any)
	if rid1pos := positions["rid1"].(map[string]any); rid1pos["x"].(float64) != 1.0 {
		t.Errorf("position x masked: %v", positions)
	}
}

// TestMaskLayerPropertyValues_Given_NoLayers_When_Masked_Then_PayloadUnchanged
func TestMaskLayerPropertyValues_Given_NoLayers_When_Masked_Then_PayloadUnchanged(t *testing.T) {
	in := json.RawMessage(`{"foo": "bar"}`)
	out := maskLayerPropertyValues(in)

	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["foo"] != "bar" {
		t.Errorf("payload without layers should pass through; got %v", got)
	}
}

// TestMemShareLinkStore_Given_Token_When_Revoke_Then_GetStillReturnsRowFlaggedRevoked
func TestMemShareLinkStore_Given_Token_When_Revoke_Then_GetStillReturnsRowFlaggedRevoked(t *testing.T) {
	s := NewMemShareLinkStore()
	link := &ShareLink{Token: "abc", GraphRID: "ri.vertex.main.graph.x", CreatedBy: "user1"}
	if err := s.Create(context.Background(), link); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Revoke(context.Background(), "abc"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	got, err := s.Get(context.Background(), "abc")
	if err != nil {
		t.Fatalf("get after revoke: %v", err)
	}
	if !got.Revoked {
		t.Errorf("revoked = false after revoke, want true")
	}
	if got.RevokedAt.IsZero() {
		t.Errorf("revokedAt is zero after revoke")
	}
}

// TestMemShareLinkStore_Given_UnknownToken_When_Revoke_Then_ErrShareLinkNotFound
func TestMemShareLinkStore_Given_UnknownToken_When_Revoke_Then_ErrShareLinkNotFound(t *testing.T) {
	s := NewMemShareLinkStore()
	err := s.Revoke(context.Background(), "missing")
	if !errors.Is(err, ErrShareLinkNotFound) {
		t.Errorf("revoke unknown got %v, want ErrShareLinkNotFound", err)
	}
}

// TestMemShareLinkStore_Given_RevokedTwice_When_Revoke_Then_Idempotent
func TestMemShareLinkStore_Given_RevokedTwice_When_Revoke_Then_Idempotent(t *testing.T) {
	s := NewMemShareLinkStore()
	link := &ShareLink{Token: "abc", GraphRID: "ri.x"}
	_ = s.Create(context.Background(), link)
	if err := s.Revoke(context.Background(), "abc"); err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if err := s.Revoke(context.Background(), "abc"); err != nil {
		t.Errorf("second revoke returned %v, want nil (idempotent)", err)
	}
}
