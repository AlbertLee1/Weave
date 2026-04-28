package dashboards

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func newRow(id, owner, name string) *Dashboard {
	return &Dashboard{
		ID:         id,
		Name:       name,
		CreatedBy:  owner,
		Definition: json.RawMessage(`{"widgets":[]}`),
	}
}

func TestMemoryStore_CreateGetListUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	for _, r := range []*Dashboard{
		newRow("id1", "alice", "Sales"),
		newRow("id2", "alice", "Ops"),
		newRow("id3", "bob", "Sales"),
	} {
		if err := s.Create(ctx, r); err != nil {
			t.Fatalf("Create(%s): %v", r.ID, err)
		}
	}

	// Same owner cannot reuse name.
	if err := s.Create(ctx, newRow("id4", "alice", "Sales")); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict for duplicate name, got %v", err)
	}
	// Different owners may reuse a name.
	if err := s.Create(ctx, newRow("id5", "carol", "Sales")); err != nil {
		t.Fatalf("cross-owner reuse should succeed: %v", err)
	}

	// Get scoped by owner — cross-user lookup is ErrNotFound on private rows.
	got, err := s.Get(ctx, "id1", "alice")
	if err != nil || got.Name != "Sales" {
		t.Fatalf("Get(id1, alice) = %v, %v", got, err)
	}
	if _, err := s.Get(ctx, "id1", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private cross-user Get: want ErrNotFound, got %v", err)
	}

	// Public sharing: setting is_public lets any authenticated caller read.
	pub := true
	if err := s.Update(ctx, "id1", "alice", Update{IsPublic: &pub}); err != nil {
		t.Fatalf("Update isPublic: %v", err)
	}
	if got, err := s.Get(ctx, "id1", "bob"); err != nil || got.Name != "Sales" {
		t.Fatalf("public cross-user Get: want %q, got (%+v, %v)", "Sales", got, err)
	}
	// Public dashboards still only mutate via their owner.
	rename := "Sales-2"
	if err := s.Update(ctx, "id1", "bob", Update{Name: &rename}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("public Update by non-owner: want ErrNotFound, got %v", err)
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
	if err := s.Update(ctx, "id1", "alice", Update{Name: &rename}); err != nil {
		t.Fatalf("Update rename: %v", err)
	}
	updated, _ := s.Get(ctx, "id1", "alice")
	if updated.Name != "Sales-2" {
		t.Fatalf("rename did not persist: %q", updated.Name)
	}

	// Update rename collides with existing name.
	collide := "Ops"
	if err := s.Update(ctx, "id1", "alice", Update{Name: &collide}); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict on rename, got %v", err)
	}

	// Update definition.
	newDef := json.RawMessage(`{"widgets":[{"id":"w1"}]}`)
	if err := s.Update(ctx, "id1", "alice", Update{Definition: &newDef}); err != nil {
		t.Fatalf("Update definition: %v", err)
	}
	updated2, _ := s.Get(ctx, "id1", "alice")
	if string(updated2.Definition) != `{"widgets":[{"id":"w1"}]}` {
		t.Fatalf("definition not updated: %s", string(updated2.Definition))
	}

	// Cross-user delete is ErrNotFound; owner delete succeeds.
	if err := s.Delete(ctx, "id1", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Delete: want ErrNotFound, got %v", err)
	}
	if err := s.Delete(ctx, "id1", "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := s.Create(ctx, newRow("id6", "alice", "Sales-2")); err != nil {
		t.Fatalf("name should be reusable after delete: %v", err)
	}
}

func TestValidateName(t *testing.T) {
	cases := map[string]bool{
		"":                                    false,
		"   ":                                 false,
		"x":                                   true,
		"My Dashboard":                        true,
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
