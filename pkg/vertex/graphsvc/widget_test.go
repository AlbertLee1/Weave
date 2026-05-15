package graphsvc

// VTX-014 unit tests for the pure widgetCompactPayload helper.
//
// Compact = strip keys that the embedded widget never needs (savedSelections,
// history) while preserving everything else byte-for-byte.

import (
	"encoding/json"
	"testing"
)

func TestWidgetCompactPayload_Given_SavedSelectionsAndHistory_When_Compacted_Then_KeysStripped(t *testing.T) {
	in := json.RawMessage(`{
		"layers":[{"id":"L1"}],
		"edges":[{"id":"E1"}],
		"positions":{"n1":{"x":1,"y":2}},
		"savedSelections":[{"id":"sel1"}],
		"history":[{"version":1}],
		"timeSettings":{"window":"7d"}
	}`)

	out := widgetCompactPayload(in)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode compact output: %v", err)
	}
	if _, present := got["savedSelections"]; present {
		t.Errorf("savedSelections should be stripped; got %v", got["savedSelections"])
	}
	if _, present := got["history"]; present {
		t.Errorf("history should be stripped; got %v", got["history"])
	}
	if _, ok := got["layers"].([]any); !ok {
		t.Errorf("layers must be preserved")
	}
	if _, ok := got["edges"].([]any); !ok {
		t.Errorf("edges must be preserved")
	}
	if _, ok := got["positions"].(map[string]any); !ok {
		t.Errorf("positions must be preserved")
	}
	if _, ok := got["timeSettings"].(map[string]any); !ok {
		t.Errorf("timeSettings must be preserved (only widget-noise keys are dropped)")
	}
}

func TestWidgetCompactPayload_Given_EmptyPayload_When_Compacted_Then_Unchanged(t *testing.T) {
	in := json.RawMessage(``)
	out := widgetCompactPayload(in)
	if len(out) != 0 {
		t.Errorf("empty in → expected empty out, got %q", string(out))
	}
}

func TestWidgetCompactPayload_Given_NoWidgetNoise_When_Compacted_Then_Roundtrip(t *testing.T) {
	in := json.RawMessage(`{"layers":[],"edges":[]}`)
	out := widgetCompactPayload(in)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := got["layers"].([]any); !ok {
		t.Errorf("layers lost")
	}
	if _, ok := got["edges"].([]any); !ok {
		t.Errorf("edges lost")
	}
}

func TestWidgetCompactPayload_Given_MalformedJSON_When_Compacted_Then_FallthroughPreserved(t *testing.T) {
	in := json.RawMessage(`not-json`)
	out := widgetCompactPayload(in)
	if string(out) != "not-json" {
		t.Errorf("malformed in should be returned as-is, got %q", string(out))
	}
}
