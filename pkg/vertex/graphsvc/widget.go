package graphsvc

// VTX-014 — Workshop-embedded widget surface.
//
// The vertex_graph widget renders inside Workshop modules. Workshop calls
// GET /api/vertex/v1/graphs/{rid}/widget for a *compact* representation of
// the graph: layers + edges + positions stay; per-user state that's only
// useful inside the Vertex workspace (savedSelections, embedded history)
// is dropped. POST /widget/save accepts an optional overrideGraphRid so
// the embedding can persist into a different graph than the one it was
// initialised from — the typical "Save As" inside a widget.

import "encoding/json"

// widgetNoiseKeys is the set of top-level payload keys the widget endpoint
// strips. Anything not listed is forwarded byte-for-byte so widget consumers
// can index into timeSettings, positions, layers, etc. without surprises.
var widgetNoiseKeys = []string{
	"savedSelections",
	"history",
}

// widgetCompactPayload returns a copy of payload with widgetNoiseKeys removed.
// Behaviour on edge cases:
//   - empty / nil payload → returned as-is (handler emits null).
//   - non-object payload (malformed, array, scalar) → returned as-is so
//     a degraded fixture still flows to the widget; the widget can decide
//     what to do with it.
//   - remarshal failure → returned as-is for the same reason.
func widgetCompactPayload(payload json.RawMessage) json.RawMessage {
	if len(payload) == 0 {
		return payload
	}
	var obj map[string]any
	if err := json.Unmarshal(payload, &obj); err != nil {
		return payload
	}
	for _, k := range widgetNoiseKeys {
		delete(obj, k)
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return payload
	}
	return out
}
