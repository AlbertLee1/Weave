package savedsearches

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func newRow(id, owner, name, ot string) *SavedSearch {
	return &SavedSearch{
		ID:         id,
		Name:       name,
		Ontology:   "main",
		ObjectType: ot,
		CreatedBy:  owner,
		Definition: json.RawMessage(`{"q":"x"}`),
	}
}

func TestMemoryStore_CreateGetListUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	// Create two rows under one owner, one under another owner.
	for _, r := range []*SavedSearch{
		newRow("id1", "alice", "Apples", "produce"),
		newRow("id2", "alice", "Bananas", "produce"),
		newRow("id3", "alice", "Other", "vendor"),
		newRow("id4", "bob", "Apples", "produce"),
	} {
		if err := s.Create(ctx, r); err != nil {
			t.Fatalf("Create(%s): %v", r.ID, err)
		}
	}

	// Same owner cannot reuse name.
	if err := s.Create(ctx, newRow("id5", "alice", "Apples", "produce")); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict for duplicate name, got %v", err)
	}
	// Different owners may reuse a name.
	if err := s.Create(ctx, newRow("id6", "carol", "Apples", "produce")); err != nil {
		t.Fatalf("cross-owner reuse should succeed: %v", err)
	}

	// Get scoped by owner.
	got, err := s.Get(ctx, "id1", "alice")
	if err != nil || got.Name != "Apples" {
		t.Fatalf("Get(id1, alice) = %v, %v", got, err)
	}
	// Foreign owner sees ErrNotFound.
	if _, err := s.Get(ctx, "id1", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user Get, got %v", err)
	}

	// List scoped to (owner, ontology, objectType).
	rows, err := s.List(ctx, "alice", "main", "produce")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "Apples" || rows[1].Name != "Bananas" {
		t.Fatalf("List returned wrong rows: %+v", rows)
	}
	// List with empty scope returns every row owned by alice.
	allRows, err := s.List(ctx, "alice", "", "")
	if err != nil {
		t.Fatalf("List unscoped: %v", err)
	}
	if len(allRows) != 3 {
		t.Fatalf("unscoped List: want 3 rows, got %d", len(allRows))
	}

	// Update: rename, keep definition.
	newName := "Apricots"
	if err := s.Update(ctx, "id1", "alice", Update{Name: &newName}); err != nil {
		t.Fatalf("Update rename: %v", err)
	}
	updated, _ := s.Get(ctx, "id1", "alice")
	if updated.Name != "Apricots" {
		t.Fatalf("rename did not persist: %q", updated.Name)
	}

	// Update rename collides with existing name.
	collide := "Bananas"
	if err := s.Update(ctx, "id1", "alice", Update{Name: &collide}); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict on rename, got %v", err)
	}

	// Update definition only.
	newDef := json.RawMessage(`{"q":"y"}`)
	if err := s.Update(ctx, "id1", "alice", Update{Definition: &newDef}); err != nil {
		t.Fatalf("Update definition: %v", err)
	}
	updated2, _ := s.Get(ctx, "id1", "alice")
	if string(updated2.Definition) != `{"q":"y"}` {
		t.Fatalf("definition not updated: %s", string(updated2.Definition))
	}

	// Update by foreign owner is ErrNotFound.
	if err := s.Update(ctx, "id1", "bob", Update{Name: &newName}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Update: want ErrNotFound, got %v", err)
	}

	// Delete: foreign owner sees ErrNotFound; owner succeeds.
	if err := s.Delete(ctx, "id1", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Delete: want ErrNotFound, got %v", err)
	}
	if err := s.Delete(ctx, "id1", "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// After delete the name is free.
	if err := s.Create(ctx, newRow("id7", "alice", "Apricots", "produce")); err != nil {
		t.Fatalf("name should be reusable after delete: %v", err)
	}
}

func TestValidateName(t *testing.T) {
	cases := map[string]bool{
		"":                                    false,
		"   ":                                 false,
		"x":                                   true,
		"ok name":                             true,
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

func TestValidateScope(t *testing.T) {
	if err := ValidateScope("", "ot"); err == nil {
		t.Error("expected error for empty ontology")
	}
	if err := ValidateScope("main", ""); err == nil {
		t.Error("expected error for empty objectType")
	}
	if err := ValidateScope("main", "ot"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
