package audit

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestRecord(t *testing.T) {
	store := NewMemoryStore()

	evt := AuditEvent{
		ActorID:      "user-123",
		Action:       "CREATE",
		ResourceType: "ObjectType",
		ResourceRID:  "ri.ontology.main.objectType.abc",
		DiffJSON:     json.RawMessage(`{"after":{"apiName":"Employee"}}`),
		IP:           "127.0.0.1",
		UserAgent:    "test-agent",
	}

	err := Record(context.Background(), store, evt)
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	events := store.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	got := events[0]

	if got.ID == "" {
		t.Error("expected non-empty ID")
	}
	if got.ActorID != "user-123" {
		t.Errorf("ActorID = %q, want %q", got.ActorID, "user-123")
	}
	if got.Action != "CREATE" {
		t.Errorf("Action = %q, want %q", got.Action, "CREATE")
	}
	if got.ResourceType != "ObjectType" {
		t.Errorf("ResourceType = %q, want %q", got.ResourceType, "ObjectType")
	}
	if got.ResourceRID != "ri.ontology.main.objectType.abc" {
		t.Errorf("ResourceRID = %q, want %q", got.ResourceRID, "ri.ontology.main.objectType.abc")
	}
	if got.IP != "127.0.0.1" {
		t.Errorf("IP = %q, want %q", got.IP, "127.0.0.1")
	}
	if got.UserAgent != "test-agent" {
		t.Errorf("UserAgent = %q, want %q", got.UserAgent, "test-agent")
	}
	if got.Timestamp.IsZero() {
		t.Error("expected non-zero Timestamp")
	}
	if time.Since(got.Timestamp) > 5*time.Second {
		t.Errorf("Timestamp too old: %v", got.Timestamp)
	}
}

func TestRecord_MultipleEvents(t *testing.T) {
	store := NewMemoryStore()

	actions := []string{"CREATE", "UPDATE", "DELETE"}
	for _, a := range actions {
		err := Record(context.Background(), store, AuditEvent{
			ActorID:      "user-1",
			Action:       a,
			ResourceType: "ObjectType",
			ResourceRID:  "ri.ontology.main.objectType.abc",
		})
		if err != nil {
			t.Fatalf("Record(%s) error = %v", a, err)
		}
	}

	events := store.Events()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	// Each event should have a unique ID
	ids := map[string]bool{}
	for _, e := range events {
		if ids[e.ID] {
			t.Errorf("duplicate event ID: %s", e.ID)
		}
		ids[e.ID] = true
	}
}

func TestRecord_NilDiffJSON(t *testing.T) {
	store := NewMemoryStore()

	err := Record(context.Background(), store, AuditEvent{
		ActorID:      "user-1",
		Action:       "DELETE",
		ResourceType: "ObjectType",
		ResourceRID:  "ri.ontology.main.objectType.abc",
	})
	if err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	got := store.Events()[0]
	if got.DiffJSON != nil {
		t.Errorf("expected nil DiffJSON, got %s", string(got.DiffJSON))
	}
}

func TestListEvents(t *testing.T) {
	store := NewMemoryStore()

	// Record events with different resource types
	_ = Record(context.Background(), store, AuditEvent{
		ActorID: "user-1", Action: "CREATE",
		ResourceType: "ObjectType", ResourceRID: "ri.ontology.main.objectType.a",
	})
	_ = Record(context.Background(), store, AuditEvent{
		ActorID: "user-2", Action: "UPDATE",
		ResourceType: "Property", ResourceRID: "ri.ontology.main.property.b",
	})
	_ = Record(context.Background(), store, AuditEvent{
		ActorID: "user-1", Action: "DELETE",
		ResourceType: "ObjectType", ResourceRID: "ri.ontology.main.objectType.c",
	})

	// List all
	all, err := store.List(context.Background(), ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 events, got %d", len(all))
	}

	// Filter by actor
	byActor, err := store.List(context.Background(), ListFilter{ActorID: "user-1"})
	if err != nil {
		t.Fatalf("List(actor) error = %v", err)
	}
	if len(byActor) != 2 {
		t.Fatalf("expected 2 events for user-1, got %d", len(byActor))
	}

	// Filter by action
	byAction, err := store.List(context.Background(), ListFilter{Action: "CREATE"})
	if err != nil {
		t.Fatalf("List(action) error = %v", err)
	}
	if len(byAction) != 1 {
		t.Fatalf("expected 1 CREATE event, got %d", len(byAction))
	}

	// Filter by resource type
	byType, err := store.List(context.Background(), ListFilter{ResourceType: "ObjectType"})
	if err != nil {
		t.Fatalf("List(resourceType) error = %v", err)
	}
	if len(byType) != 2 {
		t.Fatalf("expected 2 ObjectType events, got %d", len(byType))
	}
}
