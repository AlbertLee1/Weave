package actiontemplates

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func newRow(id, owner, actionType, name string, shared bool) *Template {
	return &Template{
		ID:         id,
		Name:       name,
		Ontology:   "main",
		ActionType: actionType,
		CreatedBy:  owner,
		Shared:     shared,
		Parameters: json.RawMessage(`{"qty":1}`),
	}
}

func TestMemoryStore_CreateGetListUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	for _, r := range []*Template{
		newRow("id1", "alice", "createOrder", "Daily Reorder", false),
		newRow("id2", "alice", "createOrder", "Bulk", false),
		newRow("id3", "alice", "shipOrder", "Express", false),
		newRow("id4", "bob", "createOrder", "Daily Reorder", false),
		newRow("id5", "carol", "createOrder", "Team Default", true),
	} {
		if err := s.Create(ctx, r); err != nil {
			t.Fatalf("Create(%s): %v", r.ID, err)
		}
	}

	if err := s.Create(ctx, newRow("id6", "alice", "createOrder", "Daily Reorder", false)); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict for duplicate (owner, actionType, name), got %v", err)
	}
	if err := s.Create(ctx, newRow("id7", "alice", "shipOrder", "Daily Reorder", false)); err != nil {
		t.Fatalf("same name under different actionType should succeed: %v", err)
	}

	got, err := s.Get(ctx, "id1", "alice")
	if err != nil || got.Name != "Daily Reorder" {
		t.Fatalf("Get(id1, alice) = %v, %v", got, err)
	}
	if _, err := s.Get(ctx, "id1", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user private Get, got %v", err)
	}
	// Shared row is visible to everyone.
	if got, err := s.Get(ctx, "id5", "alice"); err != nil || got.CreatedBy != "carol" {
		t.Fatalf("shared Get from non-owner: %v %v", got, err)
	}
	if got, err := s.Get(ctx, "id5", "bob"); err != nil || got.Shared != true {
		t.Fatalf("shared Get from another non-owner: %v %v", got, err)
	}

	rows, err := s.List(ctx, "alice", "main", "createOrder")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("alice list (createOrder) = %d rows, want 3 (Bulk, Daily Reorder, Team Default-shared)", len(rows))
	}
	names := []string{}
	for _, r := range rows {
		names = append(names, r.Name)
	}
	if names[0] != "Bulk" || names[1] != "Daily Reorder" || names[2] != "Team Default" {
		t.Fatalf("List ordering: %v", names)
	}

	bobRows, err := s.List(ctx, "bob", "main", "createOrder")
	if err != nil {
		t.Fatalf("List(bob): %v", err)
	}
	if len(bobRows) != 2 {
		t.Fatalf("bob list (createOrder) = %d rows, want 2 (his own + carol's shared)", len(bobRows))
	}

	allAlice, err := s.List(ctx, "alice", "", "")
	if err != nil {
		t.Fatalf("List(alice, unscoped): %v", err)
	}
	if len(allAlice) != 5 {
		t.Fatalf("alice unscoped list: want 5 rows (4 own + 1 shared), got %d", len(allAlice))
	}

	newName := "Reorder"
	if err := s.Update(ctx, "id1", "alice", Update{Name: &newName}); err != nil {
		t.Fatalf("Update rename: %v", err)
	}
	updated, _ := s.Get(ctx, "id1", "alice")
	if updated.Name != "Reorder" {
		t.Fatalf("rename did not persist: %q", updated.Name)
	}

	collide := "Bulk"
	if err := s.Update(ctx, "id1", "alice", Update{Name: &collide}); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict on rename, got %v", err)
	}

	newParams := json.RawMessage(`{"qty":2}`)
	if err := s.Update(ctx, "id1", "alice", Update{Parameters: &newParams}); err != nil {
		t.Fatalf("Update parameters: %v", err)
	}
	updated2, _ := s.Get(ctx, "id1", "alice")
	if string(updated2.Parameters) != `{"qty":2}` {
		t.Fatalf("parameters not updated: %s", string(updated2.Parameters))
	}

	shareOn := true
	if err := s.Update(ctx, "id1", "alice", Update{Shared: &shareOn}); err != nil {
		t.Fatalf("Update shared: %v", err)
	}
	updated3, _ := s.Get(ctx, "id1", "alice")
	if !updated3.Shared {
		t.Fatalf("shared flag not flipped: %+v", updated3)
	}
	// Now bob can see id1.
	if got, err := s.Get(ctx, "id1", "bob"); err != nil || got.ID != "id1" {
		t.Fatalf("post-share Get from non-owner: %v %v", got, err)
	}

	if err := s.Update(ctx, "id1", "bob", Update{Name: &collide}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Update even on shared row: want ErrNotFound, got %v", err)
	}

	if err := s.Delete(ctx, "id1", "bob"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user Delete: want ErrNotFound, got %v", err)
	}
	if err := s.Delete(ctx, "id1", "alice"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Reusable name after delete.
	if err := s.Create(ctx, newRow("id8", "alice", "createOrder", "Reorder", false)); err != nil {
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
	if err := ValidateScope("", "act"); err == nil {
		t.Error("expected error for empty ontology")
	}
	if err := ValidateScope("main", ""); err == nil {
		t.Error("expected error for empty actionType")
	}
	if err := ValidateScope("main", "act"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
