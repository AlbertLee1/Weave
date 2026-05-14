package oms

import (
	"encoding/json"
	"testing"
)

// VTX-077: ObjectType gains is_event + event_start_prop + event_end_prop
// metadata so the Vertex Timeline can render rows of an event-typed
// ObjectType as time bars.

func TestObjectType_Given_IsEventTrue_When_JSONMarshal_Then_FieldsRoundTrip(t *testing.T) {
	ot := ObjectType{
		RID:            "ri.ontology.main.object-type.flight-delay",
		APIName:        "FlightDelay",
		DisplayName:    "Flight Delay",
		IsEvent:        true,
		EventStartProp: "startedAt",
		EventEndProp:   "resolvedAt",
	}
	b, err := json.Marshal(ot)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got ObjectType
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.IsEvent {
		t.Errorf("IsEvent: want true, got false")
	}
	if got.EventStartProp != "startedAt" {
		t.Errorf("EventStartProp: want %q, got %q", "startedAt", got.EventStartProp)
	}
	if got.EventEndProp != "resolvedAt" {
		t.Errorf("EventEndProp: want %q, got %q", "resolvedAt", got.EventEndProp)
	}
}

func TestObjectType_Given_NonEvent_When_JSONMarshal_Then_EventFieldsOmitted(t *testing.T) {
	ot := ObjectType{
		RID:         "ri.ontology.main.object-type.airport",
		APIName:     "Airport",
		DisplayName: "Airport",
	}
	b, err := json.Marshal(ot)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := raw["isEvent"]; ok {
		// When isEvent is false the JSON should omit it (omitempty) so the
		// wire payload stays small for non-event ObjectTypes.
		t.Errorf("expected isEvent to be omitted, got %v", raw["isEvent"])
	}
	if _, ok := raw["eventStartProp"]; ok {
		t.Errorf("expected eventStartProp to be omitted, got %v", raw["eventStartProp"])
	}
	if _, ok := raw["eventEndProp"]; ok {
		t.Errorf("expected eventEndProp to be omitted, got %v", raw["eventEndProp"])
	}
}

func TestObjectType_Given_EventWithEmptyEndProp_When_JSONMarshal_Then_OnlyStartProp(t *testing.T) {
	// A point-in-time event: start prop set, end prop empty (e.g. system
	// alerts with no resolution timestamp). The end prop should drop from
	// the JSON via omitempty.
	ot := ObjectType{
		RID:            "ri.ontology.main.object-type.alert",
		APIName:        "Alert",
		DisplayName:    "Alert",
		IsEvent:        true,
		EventStartProp: "firedAt",
	}
	b, err := json.Marshal(ot)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if raw["isEvent"] != true {
		t.Errorf("isEvent: want true, got %v", raw["isEvent"])
	}
	if raw["eventStartProp"] != "firedAt" {
		t.Errorf("eventStartProp: want firedAt, got %v", raw["eventStartProp"])
	}
	if _, ok := raw["eventEndProp"]; ok {
		t.Errorf("expected eventEndProp to be omitted on empty, got %v", raw["eventEndProp"])
	}
}
