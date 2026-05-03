package quiver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func newRow(rid, owner, name string) *Dashboard {
	return &Dashboard{
		RID:    rid,
		Name:   name,
		Owner:  owner,
		Config: json.RawMessage(`{"series":[]}`),
	}
}

func TestMemoryStore_SaveGetListUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	for _, r := range []*Dashboard{
		newRow("rid1", "alice", "Sales"),
		newRow("rid2", "alice", "Ops"),
		newRow("rid3", "bob", "Sales"),
	} {
		if err := s.Save(ctx, r); err != nil {
			t.Fatalf("Save(%s): %v", r.RID, err)
		}
	}

	// Same owner cannot reuse name on a fresh row.
	if err := s.Save(ctx, newRow("rid4", "alice", "Sales")); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict for duplicate name, got %v", err)
	}
	// Different owners may reuse a name.
	if err := s.Save(ctx, newRow("rid5", "carol", "Sales")); err != nil {
		t.Fatalf("cross-owner reuse should succeed: %v", err)
	}

	// Get scoped by owner — cross-user lookup is ErrNotFound.
	got, err := s.Get(ctx, "rid1", "alice")
	if err != nil || got.Name != "Sales" {
		t.Fatalf("Get(rid1, alice) = %v, %v", got, err)
	}
	if _, err := s.Get(ctx, "rid1", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private cross-user Get: want ErrNotFound, got %v", err)
	}

	// GetByRID succeeds for any caller — the share surface.
	if got, err := s.GetByRID(ctx, "rid1"); err != nil || got.Owner != "alice" {
		t.Fatalf("GetByRID(rid1): want alice owner, got (%+v, %v)", got, err)
	}

	// List returns the caller's dashboards, sorted by name.
	rows, err := s.List(ctx, "alice")
	if err != nil {
		t.Fatalf("List(alice): %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "Ops" || rows[1].Name != "Sales" {
		t.Fatalf("List(alice) returned wrong rows: %+v", rows)
	}

	// Update: rename.
	rename := "Sales-2"
	if err := s.Update(ctx, "rid1", "alice", Update{Name: &rename}); err != nil {
		t.Fatalf("Update rename: %v", err)
	}
	updated, _ := s.Get(ctx, "rid1", "alice")
	if updated.Name != "Sales-2" {
		t.Fatalf("rename did not persist: %q", updated.Name)
	}

	// Update rename collides with existing name.
	collide := "Ops"
	if err := s.Update(ctx, "rid1", "alice", Update{Name: &collide}); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict on rename, got %v", err)
	}

	// Update config.
	newCfg := json.RawMessage(`{"series":[{"id":"s1"}]}`)
	if err := s.Update(ctx, "rid1", "alice", Update{Config: &newCfg}); err != nil {
		t.Fatalf("Update config: %v", err)
	}
	updated2, _ := s.Get(ctx, "rid1", "alice")
	if string(updated2.Config) != `{"series":[{"id":"s1"}]}` {
		t.Fatalf("config not updated: %s", string(updated2.Config))
	}

	// Cross-user mutate is ErrNotFound; owner mutate succeeds.
	if err := s.Update(ctx, "rid1", "bob", Update{Name: &rename}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Update: want ErrNotFound, got %v", err)
	}
	if err := s.Delete(ctx, "rid1", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Delete: want ErrNotFound, got %v", err)
	}
	if err := s.Delete(ctx, "rid1", "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// After delete the share-link surface 404s too.
	if _, err := s.GetByRID(ctx, "rid1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetByRID after delete: want ErrNotFound, got %v", err)
	}
	// Deleting a row frees its name for re-use.
	if err := s.Save(ctx, newRow("rid6", "alice", "Sales-2")); err != nil {
		t.Fatalf("name should be reusable after delete: %v", err)
	}
}

func TestValidateName(t *testing.T) {
	cases := map[string]bool{
		"":                                    false,
		"   ":                                 false,
		"x":                                   true,
		"My Quiver Dashboard":                 true,
		string(make([]byte, MaxNameLength)):   true,
		string(make([]byte, MaxNameLength+1)): false,
	}
	for name, valid := range cases {
		err := ValidateName(name)
		if valid && err != nil {
			t.Errorf("ValidateName(%q): unexpected error %v", name, err)
		}
		if !valid && err == nil {
			t.Errorf("ValidateName(%q): expected error", name)
		}
	}
}
