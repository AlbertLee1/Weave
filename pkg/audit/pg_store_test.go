//go:build integration

package audit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
)

func TestPGStore_InsertAndList(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	store := NewPGStore(pg.Pool)
	ctx := context.Background()

	// Insert an event with diff
	diff := json.RawMessage(`{"after":{"apiName":"Employee"}}`)
	evt := AuditEvent{
		ActorID:      "user-42",
		Action:       "CREATE",
		ResourceType: "ObjectType",
		ResourceRID:  "ri.ontology.main.objectType.emp",
		DiffJSON:     diff,
		IP:           "10.0.0.1",
		UserAgent:    "integration-test",
	}
	if err := Record(ctx, store, evt); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	// Insert an event without diff
	evt2 := AuditEvent{
		ActorID:      "user-42",
		Action:       "DELETE",
		ResourceType: "Property",
		ResourceRID:  "ri.ontology.main.property.foo",
	}
	if err := Record(ctx, store, evt2); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	// Insert from a different actor
	evt3 := AuditEvent{
		ActorID:      "user-99",
		Action:       "UPDATE",
		ResourceType: "ObjectType",
		ResourceRID:  "ri.ontology.main.objectType.emp",
		DiffJSON:     json.RawMessage(`{"before":{"status":"ACTIVE"},"after":{"status":"DEPRECATED"}}`),
	}
	if err := Record(ctx, store, evt3); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	// List all
	all, err := store.List(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 events, got %d", len(all))
	}

	// Results are ordered by ts DESC
	if all[0].Action != "UPDATE" {
		t.Errorf("expected most recent first (UPDATE), got %s", all[0].Action)
	}

	// Filter by actor
	byActor, err := store.List(ctx, ListFilter{ActorID: "user-42"})
	if err != nil {
		t.Fatalf("List(actor) error = %v", err)
	}
	if len(byActor) != 2 {
		t.Fatalf("expected 2 events for user-42, got %d", len(byActor))
	}

	// Filter by action
	byAction, err := store.List(ctx, ListFilter{Action: "CREATE"})
	if err != nil {
		t.Fatalf("List(action) error = %v", err)
	}
	if len(byAction) != 1 {
		t.Fatalf("expected 1 CREATE event, got %d", len(byAction))
	}
	// Compare semantically: JSONB normalises whitespace on round-trip.
	var gotDiff, wantDiff map[string]any
	if err := json.Unmarshal(byAction[0].DiffJSON, &gotDiff); err != nil {
		t.Fatalf("unmarshal got DiffJSON: %v", err)
	}
	if err := json.Unmarshal([]byte(`{"after":{"apiName":"Employee"}}`), &wantDiff); err != nil {
		t.Fatalf("unmarshal want DiffJSON: %v", err)
	}
	gotBytes, _ := json.Marshal(gotDiff)
	wantBytes, _ := json.Marshal(wantDiff)
	if string(gotBytes) != string(wantBytes) {
		t.Errorf("DiffJSON mismatch: got %s, want %s", string(gotBytes), string(wantBytes))
	}

	// Filter by resource type
	byType, err := store.List(ctx, ListFilter{ResourceType: "ObjectType"})
	if err != nil {
		t.Fatalf("List(type) error = %v", err)
	}
	if len(byType) != 2 {
		t.Fatalf("expected 2 ObjectType events, got %d", len(byType))
	}

	// Filter by time range
	now := time.Now()
	past := now.Add(-1 * time.Hour)
	byTime, err := store.List(ctx, ListFilter{From: &past, To: &now})
	if err != nil {
		t.Fatalf("List(timeRange) error = %v", err)
	}
	if len(byTime) != 3 {
		t.Fatalf("expected 3 events in last hour, got %d", len(byTime))
	}

	// Null diff roundtrips correctly
	delEvents, err := store.List(ctx, ListFilter{Action: "DELETE"})
	if err != nil {
		t.Fatalf("List(DELETE) error = %v", err)
	}
	if len(delEvents) != 1 {
		t.Fatalf("expected 1 DELETE event, got %d", len(delEvents))
	}
	if delEvents[0].DiffJSON != nil {
		t.Errorf("expected nil DiffJSON for DELETE event, got %s", string(delEvents[0].DiffJSON))
	}
}
