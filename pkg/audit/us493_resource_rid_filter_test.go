package audit

import (
	"context"
	"testing"
)

// TestUS493_ListFilter_ResourceRID_FiltersExactMatch is the PRD literal test
// for US-493 acceptance criterion "GET /api/admin/audit 支持按 actor /
// resourceRid / 时间范围筛选". It pins down that ListFilter.ResourceRID is an
// exact-match clause (not a prefix / substring), and that it composes with
// the existing ActorID + ResourceType filters.
func TestUS493_ListFilter_ResourceRID_FiltersExactMatch(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	want := "ri.ontology.main.objectType.target"
	other := "ri.ontology.main.objectType.other"

	for _, evt := range []AuditEvent{
		{ActorID: "user-a", Action: "CREATE", ResourceType: "ObjectType", ResourceRID: want},
		{ActorID: "user-a", Action: "UPDATE", ResourceType: "ObjectType", ResourceRID: want},
		{ActorID: "user-b", Action: "DELETE", ResourceType: "ObjectType", ResourceRID: other},
		{ActorID: "user-a", Action: "CREATE", ResourceType: "Property", ResourceRID: "ri.ontology.main.property.x"},
	} {
		if err := Record(ctx, store, evt); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	got, err := store.List(ctx, ListFilter{ResourceRID: want})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ResourceRID filter returned %d events, want 2", len(got))
	}
	for _, e := range got {
		if e.ResourceRID != want {
			t.Errorf("event has ResourceRID=%q, want %q", e.ResourceRID, want)
		}
	}

	// composes with actor
	gotActor, err := store.List(ctx, ListFilter{ResourceRID: want, ActorID: "user-a"})
	if err != nil {
		t.Fatalf("List(actor+rid): %v", err)
	}
	if len(gotActor) != 2 {
		t.Fatalf("actor+rid filter returned %d events, want 2", len(gotActor))
	}

	gotMiss, err := store.List(ctx, ListFilter{ResourceRID: "ri.does.not.exist"})
	if err != nil {
		t.Fatalf("List(miss): %v", err)
	}
	if len(gotMiss) != 0 {
		t.Fatalf("non-existent RID returned %d events, want 0", len(gotMiss))
	}
}
