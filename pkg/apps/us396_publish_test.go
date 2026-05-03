package apps

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMemoryStore_PublishStampsCurrentVersion(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	view, err := s.Publish(ctx, a.RID, "alice", "alice")
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if view == nil {
		t.Fatalf("Publish view is nil")
	}
	if view.PublishedVersion != 1 || view.RID != a.RID || view.Name != "Console" {
		t.Fatalf("unexpected view shape: %+v", view)
	}
	if view.PublishedAt.IsZero() {
		t.Fatalf("PublishedAt should be stamped, got zero")
	}
	if view.PublishedBy != "alice" {
		t.Fatalf("PublishedBy=%q, want alice", view.PublishedBy)
	}
	row, err := s.Get(ctx, a.RID, "alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.PublishedVersion == nil || *row.PublishedVersion != 1 {
		t.Fatalf("live row should reflect publish pin, got %+v", row.PublishedVersion)
	}
}

func TestMemoryStore_PublishOwnerOnly(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Publish(ctx, a.RID, "bob", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Publish by non-owner: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_PublishUnknownRID(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if _, err := s.Publish(ctx, "ri.app.main.app.missing", "alice", "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Publish missing: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_RepublishAdvancesPin(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	first, err := s.Publish(ctx, a.RID, "alice", "alice")
	if err != nil {
		t.Fatalf("Publish #1: %v", err)
	}
	if first.PublishedVersion != 1 {
		t.Fatalf("first publish should pin v1, got %d", first.PublishedVersion)
	}
	rename := "Console v2"
	if err := s.Update(ctx, a.RID, "alice", Update{Name: &rename}, "alice"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	second, err := s.Publish(ctx, a.RID, "alice", "alice")
	if err != nil {
		t.Fatalf("Publish #2: %v", err)
	}
	if second.PublishedVersion != 2 {
		t.Fatalf("re-publish should advance pin to v2, got %d", second.PublishedVersion)
	}
	if second.Name != "Console v2" {
		t.Fatalf("re-publish should reflect renamed snapshot, got %q", second.Name)
	}
}

func TestMemoryStore_GetPublishedReturnsSnapshot(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Publish(ctx, a.RID, "alice", "alice"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	view, err := s.GetPublished(ctx, a.RID)
	if err != nil {
		t.Fatalf("GetPublished: %v", err)
	}
	if view.RID != a.RID || view.PublishedVersion != 1 || view.Name != "Console" {
		t.Fatalf("unexpected view: %+v", view)
	}
	if len(view.LayoutJSON) == 0 {
		t.Fatalf("expected layout payload")
	}
}

func TestMemoryStore_GetPublishedNeverPublished(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.GetPublished(ctx, a.RID); !errors.Is(err, ErrNotPublished) {
		t.Fatalf("GetPublished unpublished: want ErrNotPublished, got %v", err)
	}
}

func TestMemoryStore_GetPublishedUnknownRID(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if _, err := s.GetPublished(ctx, "ri.app.main.app.missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetPublished missing: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_GetPublishedReturnsPinnedSnapshotNotLatest(t *testing.T) {
	// After publishing v1, an Update to v2 (without re-publish) MUST
	// leave the published view stuck on v1.
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Publish(ctx, a.RID, "alice", "alice"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	rename := "Console v2"
	if err := s.Update(ctx, a.RID, "alice", Update{Name: &rename}, "alice"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	view, err := s.GetPublished(ctx, a.RID)
	if err != nil {
		t.Fatalf("GetPublished: %v", err)
	}
	if view.PublishedVersion != 1 {
		t.Fatalf("published pin should still be v1, got %d", view.PublishedVersion)
	}
	if view.Name != "Console" {
		t.Fatalf("published snapshot should still be Console, got %q", view.Name)
	}
}

func TestMemoryStore_UnpublishClearsPin(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Publish(ctx, a.RID, "alice", "alice"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := s.Unpublish(ctx, a.RID, "alice"); err != nil {
		t.Fatalf("Unpublish: %v", err)
	}
	row, err := s.Get(ctx, a.RID, "alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if row.PublishedVersion != nil || row.PublishedAt != nil || row.PublishedBy != nil {
		t.Fatalf("Unpublish should clear all three columns, got %+v", row)
	}
	if _, err := s.GetPublished(ctx, a.RID); !errors.Is(err, ErrNotPublished) {
		t.Fatalf("GetPublished after Unpublish: want ErrNotPublished, got %v", err)
	}
}

func TestMemoryStore_UnpublishOwnerOnly(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := s.Publish(ctx, a.RID, "alice", "alice"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := s.Unpublish(ctx, a.RID, "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Unpublish by non-owner: want ErrNotFound, got %v", err)
	}
	// Still published.
	if _, err := s.GetPublished(ctx, a.RID); err != nil {
		t.Fatalf("GetPublished should still succeed, got %v", err)
	}
}

func TestAppJSON_OmitsPublishFieldsWhenUnset(t *testing.T) {
	// The pointer fields with omitempty must NOT serialise to "null"
	// for never-published Apps.
	a := &App{RID: "ri.app.main.app.1", Name: "X", OwnerID: "alice", Version: 1}
	bs, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(bs)
	if strings.Contains(got, `publishedVersion`) ||
		strings.Contains(got, `publishedAt`) ||
		strings.Contains(got, `publishedBy`) {
		t.Fatalf("unset publish fields should be omitted, got %s", got)
	}
}
