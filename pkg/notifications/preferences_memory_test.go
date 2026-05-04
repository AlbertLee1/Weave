package notifications

import (
	"context"
	"testing"
)

func TestMemoryPreferenceStore_UpsertAndList(t *testing.T) {
	store := NewMemoryPreferenceStore()
	ctx := context.Background()

	if err := store.Upsert(ctx, &Preference{
		UserID: "user:alice", Channel: "email", Enabled: true,
	}); err != nil {
		t.Fatalf("Upsert email: %v", err)
	}
	if err := store.Upsert(ctx, &Preference{
		UserID: "user:alice", Channel: "slack", Enabled: true, Target: "https://hooks.slack.com/...",
	}); err != nil {
		t.Fatalf("Upsert slack: %v", err)
	}

	prefs, err := store.ListByUser(ctx, "user:alice")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(prefs) != 2 {
		t.Fatalf("want 2 prefs, got %d", len(prefs))
	}
	// Sorted by channel: "email" < "slack"
	if prefs[0].Channel != "email" || prefs[1].Channel != "slack" {
		t.Errorf("unstable sort: %v", prefs)
	}
	if prefs[1].Target != "https://hooks.slack.com/..." {
		t.Errorf("Target lost: %v", prefs[1])
	}
	if prefs[0].CreatedAt.IsZero() {
		t.Errorf("CreatedAt should be stamped")
	}
}

func TestMemoryPreferenceStore_UpsertPreservesCreatedAt(t *testing.T) {
	store := NewMemoryPreferenceStore()
	ctx := context.Background()

	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "email", Enabled: true})
	first, _ := store.ListByUser(ctx, "u")
	createdAt := first[0].CreatedAt

	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "email", Enabled: false})
	second, _ := store.ListByUser(ctx, "u")
	if !second[0].CreatedAt.Equal(createdAt) {
		t.Errorf("CreatedAt should survive update: %v vs %v", second[0].CreatedAt, createdAt)
	}
	if second[0].UpdatedAt.Before(createdAt) {
		t.Errorf("UpdatedAt should advance: %v vs %v", second[0].UpdatedAt, createdAt)
	}
	if second[0].Enabled {
		t.Errorf("Enabled should flip to false")
	}
}

func TestMemoryPreferenceStore_Delete(t *testing.T) {
	store := NewMemoryPreferenceStore()
	ctx := context.Background()

	_ = store.Upsert(ctx, &Preference{UserID: "u", Channel: "slack", Enabled: true, Target: "x"})
	if err := store.Delete(ctx, "u", "slack"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	prefs, _ := store.ListByUser(ctx, "u")
	if len(prefs) != 0 {
		t.Fatalf("Delete should remove the row, got %v", prefs)
	}

	// Idempotent
	if err := store.Delete(ctx, "u", "slack"); err != nil {
		t.Errorf("Delete on missing row should be idempotent, got %v", err)
	}
}

func TestMemoryPreferenceStore_UnknownUser(t *testing.T) {
	store := NewMemoryPreferenceStore()
	prefs, err := store.ListByUser(context.Background(), "nobody")
	if err != nil {
		t.Fatalf("ListByUser(nobody): %v", err)
	}
	if len(prefs) != 0 {
		t.Errorf("unknown user should yield empty slice, got %v", prefs)
	}
}
