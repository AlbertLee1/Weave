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

// TestPGStore_ListBeforeDeleteBefore exercises the US-269 retention
// helpers. ListBefore must page by chain_seq ASC and respect the
// timestamp cutoff; DeleteBefore must remove exactly the rows older
// than the cutoff and leave the live chain untouched.
func TestPGStore_ListBeforeDeleteBefore(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migrations: %v", err)
	}

	store := NewPGStore(pg.Pool)
	ctx := context.Background()

	base := time.Now().UTC().Add(-100 * time.Hour)
	for i := 0; i < 6; i++ {
		evt := AuditEvent{
			ActorID:      "user-42",
			Action:       "CREATE",
			ResourceType: "ObjectType",
			ResourceRID:  "ri.ontology.main.objectType.emp",
			Timestamp:    base.Add(time.Duration(i) * 10 * time.Hour),
		}
		if err := Record(ctx, store, evt); err != nil {
			t.Fatalf("Record[%d]: %v", i, err)
		}
	}

	// Cutoff = base + 35h ⇒ rows at t=0,10,20,30 expire (4 rows),
	// rows at t=40,50 survive.
	cutoff := base.Add(35 * time.Hour)

	page1, err := store.ListBefore(ctx, cutoff, 0, 3)
	if err != nil {
		t.Fatalf("ListBefore page1: %v", err)
	}
	if len(page1) != 3 {
		t.Fatalf("page1 len=%d want 3", len(page1))
	}
	for i := 0; i < len(page1); i++ {
		if page1[i].ChainSeq != int64(i+1) {
			t.Fatalf("page1[%d].ChainSeq=%d want %d", i, page1[i].ChainSeq, i+1)
		}
	}

	page2, err := store.ListBefore(ctx, cutoff, page1[len(page1)-1].ChainSeq, 3)
	if err != nil {
		t.Fatalf("ListBefore page2: %v", err)
	}
	if len(page2) != 1 {
		t.Fatalf("page2 len=%d want 1", len(page2))
	}
	if page2[0].ChainSeq != 4 {
		t.Fatalf("page2[0].ChainSeq=%d want 4", page2[0].ChainSeq)
	}

	empty, err := store.ListBefore(ctx, cutoff, page2[0].ChainSeq, 3)
	if err != nil {
		t.Fatalf("ListBefore drain: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("drain len=%d want 0", len(empty))
	}

	n, err := store.DeleteBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteBefore: %v", err)
	}
	if n != 4 {
		t.Fatalf("DeleteBefore n=%d want 4", n)
	}

	remaining, err := store.List(ctx, ListFilter{})
	if err != nil {
		t.Fatalf("List after delete: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("remaining=%d want 2", len(remaining))
	}
	// Surviving rows keep their chain_seq (5, 6) so future inserts
	// chain off the live tail.
	for _, e := range remaining {
		if e.ChainSeq != 5 && e.ChainSeq != 6 {
			t.Fatalf("unexpected surviving ChainSeq=%d", e.ChainSeq)
		}
	}

	// Deleting again with the same cutoff is a no-op.
	n2, err := store.DeleteBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteBefore idempotent: %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second DeleteBefore n=%d want 0", n2)
	}

	// Zero-limit ListBefore returns nil (defensive: no work rather
	// than PG "LIMIT 0" round-trip).
	zero, err := store.ListBefore(ctx, cutoff, 0, 0)
	if err != nil {
		t.Fatalf("ListBefore limit=0: %v", err)
	}
	if zero != nil {
		t.Fatalf("ListBefore limit=0 returned %d rows, want nil", len(zero))
	}
}
