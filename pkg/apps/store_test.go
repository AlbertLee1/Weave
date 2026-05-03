package apps

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

const validLayout = `{"type":"row","children":[{"type":"col","width":12,"child":{"type":"component","componentType":"text","props":{"text":"hello"}}}]}`

func newApp(rid, owner, name string) *App {
	return &App{
		RID:        rid,
		Name:       name,
		OwnerID:    owner,
		LayoutJSON: json.RawMessage(validLayout),
	}
}

func TestMemoryStore_CreateStampsVersionOneAndHistoryRow(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	app := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, app, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if app.Version != 1 {
		t.Fatalf("Create should stamp Version=1, got %d", app.Version)
	}
	versions, err := s.ListVersions(ctx, app.RID, "alice")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("expected one v1 history row, got %+v", versions)
	}
}

func TestMemoryStore_CreateRejectsInvalidLayout(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	app := &App{
		RID:        "ri.app.main.app.bad",
		Name:       "Bad",
		OwnerID:    "alice",
		LayoutJSON: json.RawMessage(`{"type":"row"}`), // missing children
	}
	err := s.Create(ctx, app, "alice")
	if !errors.Is(err, ErrInvalidLayout) {
		t.Fatalf("expected ErrInvalidLayout, got %v", err)
	}
}

func TestMemoryStore_CreateRejectsDuplicateName(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.Create(ctx, newApp("ri.app.main.app.1", "alice", "Console"), "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	err := s.Create(ctx, newApp("ri.app.main.app.2", "alice", "Console"), "alice")
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict, got %v", err)
	}
	// Different owners may pick the same name.
	if err := s.Create(ctx, newApp("ri.app.main.app.3", "bob", "Console"), "bob"); err != nil {
		t.Fatalf("cross-owner reuse: %v", err)
	}
}

func TestMemoryStore_GetReturnsLiveRow(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := s.Get(ctx, a.RID, "alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Console" || got.Version != 1 {
		t.Fatalf("unexpected row: %+v", got)
	}
	if _, err := s.Get(ctx, a.RID, "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Get: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_UpdateBumpsVersionAndAppendsHistory(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	newName := "Console v2"
	newLayout := json.RawMessage(`{"type":"row","children":[{"type":"col","width":6,"child":{"type":"component","componentType":"chart"}},{"type":"col","width":6,"child":{"type":"component","componentType":"table"}}]}`)
	if err := s.Update(ctx, a.RID, "alice", Update{Name: &newName, LayoutJSON: &newLayout}, "alice"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := s.Get(ctx, a.RID, "alice")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != newName || got.Version != 2 {
		t.Fatalf("expected v2 with new name, got %+v", got)
	}
	versions, err := s.ListVersions(ctx, a.RID, "alice")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 history rows, got %d", len(versions))
	}
	// Most-recent row first.
	if versions[0].Version != 2 || versions[1].Version != 1 {
		t.Fatalf("history not ordered desc: %+v", versions)
	}
}

func TestMemoryStore_UpdateRejectsInvalidLayout(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	bad := json.RawMessage(`{"type":"col"}`) // bare col, missing width/child
	err := s.Update(ctx, a.RID, "alice", Update{LayoutJSON: &bad}, "alice")
	if !errors.Is(err, ErrInvalidLayout) {
		t.Fatalf("expected ErrInvalidLayout, got %v", err)
	}
	// Live row should be unchanged.
	got, _ := s.Get(ctx, a.RID, "alice")
	if got.Version != 1 {
		t.Fatalf("Version should not have bumped on invalid layout, got %d", got.Version)
	}
}

func TestMemoryStore_UpdateNameOnlyStillBumpsVersion(t *testing.T) {
	// Renaming is a versioned event so the audit trail reflects it.
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	rename := "Renamed"
	if err := s.Update(ctx, a.RID, "alice", Update{Name: &rename}, "alice"); err != nil {
		t.Fatalf("Update name: %v", err)
	}
	got, _ := s.Get(ctx, a.RID, "alice")
	if got.Version != 2 || got.Name != "Renamed" {
		t.Fatalf("expected v2/Renamed, got %+v", got)
	}
}

func TestMemoryStore_UpdateNoOp(t *testing.T) {
	// A no-op update should still succeed (DTO with both fields nil).
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Update(ctx, a.RID, "alice", Update{}, "alice"); err != nil {
		t.Fatalf("no-op Update: %v", err)
	}
	got, _ := s.Get(ctx, a.RID, "alice")
	// Still bumps version because every Update is recorded as a snapshot.
	if got.Version != 2 {
		t.Fatalf("expected v2, got %d", got.Version)
	}
}

func TestMemoryStore_UpdateNameCollision(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.Create(ctx, newApp("ri.app.main.app.1", "alice", "First"), "alice"); err != nil {
		t.Fatalf("Create 1: %v", err)
	}
	if err := s.Create(ctx, newApp("ri.app.main.app.2", "alice", "Second"), "alice"); err != nil {
		t.Fatalf("Create 2: %v", err)
	}
	collide := "First"
	err := s.Update(ctx, "ri.app.main.app.2", "alice", Update{Name: &collide}, "alice")
	if !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict, got %v", err)
	}
}

func TestMemoryStore_List(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.Create(ctx, newApp("ri.app.main.app.1", "alice", "Beta"), "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Create(ctx, newApp("ri.app.main.app.2", "alice", "Alpha"), "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Create(ctx, newApp("ri.app.main.app.3", "bob", "Carol"), "bob"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	rows, err := s.List(ctx, "alice")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "Alpha" || rows[1].Name != "Beta" {
		t.Fatalf("unexpected list: %+v", rows)
	}
}

func TestMemoryStore_DeleteCascadesVersions(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(ctx, a.RID, "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, a.RID, "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: want ErrNotFound, got %v", err)
	}
	if _, err := s.ListVersions(ctx, a.RID, "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ListVersions after Delete: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_DeleteCrossOwnerForbidden(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.Create(ctx, newApp("ri.app.main.app.1", "alice", "Console"), "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := s.Delete(ctx, "ri.app.main.app.1", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-owner Delete: want ErrNotFound, got %v", err)
	}
}

func TestMemoryStore_GetVersion(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	a := newApp("ri.app.main.app.1", "alice", "Console")
	if err := s.Create(ctx, a, "alice"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	rename := "v2"
	if err := s.Update(ctx, a.RID, "alice", Update{Name: &rename}, "alice"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	v1, err := s.GetVersion(ctx, a.RID, 1, "alice")
	if err != nil {
		t.Fatalf("GetVersion 1: %v", err)
	}
	if v1.Name != "Console" {
		t.Fatalf("v1 name should be Console, got %q", v1.Name)
	}
	v2, err := s.GetVersion(ctx, a.RID, 2, "alice")
	if err != nil {
		t.Fatalf("GetVersion 2: %v", err)
	}
	if v2.Name != "v2" {
		t.Fatalf("v2 name should be v2, got %q", v2.Name)
	}
	if _, err := s.GetVersion(ctx, a.RID, 99, "alice"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing version: want ErrNotFound, got %v", err)
	}
}
