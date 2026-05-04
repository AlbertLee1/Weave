package actiontemplates

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func newRow(id, owner, actionType, name string, scope string) *Template {
	return &Template{
		ID:         id,
		Name:       name,
		Ontology:   "main",
		ActionType: actionType,
		CreatedBy:  owner,
		Scope:      scope,
		Shared:     SharedFromScope(scope),
		Parameters: json.RawMessage(`{"qty":1}`),
	}
}

func TestMemoryStore_CreateGetListUpdateDelete(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	for _, r := range []*Template{
		newRow("id1", "alice", "createOrder", "Daily Reorder", ScopePrivate),
		newRow("id2", "alice", "createOrder", "Bulk", ScopePrivate),
		newRow("id3", "alice", "shipOrder", "Express", ScopePrivate),
		newRow("id4", "bob", "createOrder", "Daily Reorder", ScopePrivate),
		newRow("id5", "carol", "createOrder", "Team Default", ScopePublic),
	} {
		if err := s.Create(ctx, r); err != nil {
			t.Fatalf("Create(%s): %v", r.ID, err)
		}
	}

	if err := s.Create(ctx, newRow("id6", "alice", "createOrder", "Daily Reorder", ScopePrivate)); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("expected ErrNameConflict for duplicate (owner, actionType, name), got %v", err)
	}
	if err := s.Create(ctx, newRow("id7", "alice", "shipOrder", "Daily Reorder", ScopePrivate)); err != nil {
		t.Fatalf("same name under different actionType should succeed: %v", err)
	}

	aliceVis := Visibility{CallerID: "alice"}
	bobVis := Visibility{CallerID: "bob"}

	got, err := s.Get(ctx, "id1", aliceVis)
	if err != nil || got.Name != "Daily Reorder" {
		t.Fatalf("Get(id1, alice) = %v, %v", got, err)
	}
	if _, err := s.Get(ctx, "id1", bobVis); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-user private Get, got %v", err)
	}
	// PUBLIC row is visible to everyone.
	if got, err := s.Get(ctx, "id5", aliceVis); err != nil || got.CreatedBy != "carol" {
		t.Fatalf("public Get from non-owner: %v %v", got, err)
	}
	publicRow, err := s.Get(ctx, "id5", bobVis)
	if err != nil || publicRow.Scope != ScopePublic {
		t.Fatalf("public Get from another non-owner: %v %v", publicRow, err)
	}
	if publicRow.Shared != true {
		t.Fatalf("public row should round-trip Shared=true: %+v", publicRow)
	}

	rows, err := s.List(ctx, aliceVis, "main", "createOrder")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("alice list (createOrder) = %d rows, want 3 (Bulk, Daily Reorder, Team Default-public)", len(rows))
	}
	names := []string{}
	for _, r := range rows {
		names = append(names, r.Name)
	}
	if names[0] != "Bulk" || names[1] != "Daily Reorder" || names[2] != "Team Default" {
		t.Fatalf("List ordering: %v", names)
	}

	bobRows, err := s.List(ctx, bobVis, "main", "createOrder")
	if err != nil {
		t.Fatalf("List(bob): %v", err)
	}
	if len(bobRows) != 2 {
		t.Fatalf("bob list (createOrder) = %d rows, want 2 (his own + carol's public)", len(bobRows))
	}

	allAlice, err := s.List(ctx, aliceVis, "", "")
	if err != nil {
		t.Fatalf("List(alice, unscoped): %v", err)
	}
	if len(allAlice) != 5 {
		t.Fatalf("alice unscoped list: want 5 rows (4 own + 1 public), got %d", len(allAlice))
	}

	newName := "Reorder"
	if err := s.Update(ctx, "id1", "alice", Update{Name: &newName}); err != nil {
		t.Fatalf("Update rename: %v", err)
	}
	updated, _ := s.Get(ctx, "id1", aliceVis)
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
	updated2, _ := s.Get(ctx, "id1", aliceVis)
	if string(updated2.Parameters) != `{"qty":2}` {
		t.Fatalf("parameters not updated: %s", string(updated2.Parameters))
	}

	publicScope := ScopePublic
	if err := s.Update(ctx, "id1", "alice", Update{Scope: &publicScope}); err != nil {
		t.Fatalf("Update scope: %v", err)
	}
	updated3, _ := s.Get(ctx, "id1", aliceVis)
	if updated3.Scope != ScopePublic || !updated3.Shared {
		t.Fatalf("scope flip did not persist: %+v", updated3)
	}
	// Now bob can see id1.
	if got, err := s.Get(ctx, "id1", bobVis); err != nil || got.ID != "id1" {
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
	if err := s.Create(ctx, newRow("id8", "alice", "createOrder", "Reorder", ScopePrivate)); err != nil {
		t.Fatalf("name should be reusable after delete: %v", err)
	}
}

func TestMemoryStore_TeamScopeVisibility(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()

	// alice creates a TEAM-scoped row; bob is a teammate, carol is not.
	if err := s.Create(ctx, newRow("id-team", "alice", "createOrder", "Team Run", ScopeTeam)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	bobVis := Visibility{CallerID: "bob", Teammates: []string{"alice"}}
	if got, err := s.Get(ctx, "id-team", bobVis); err != nil || got.ID != "id-team" {
		t.Fatalf("teammate Get: %v %v", got, err)
	}
	bobRows, err := s.List(ctx, bobVis, "main", "createOrder")
	if err != nil {
		t.Fatalf("teammate List: %v", err)
	}
	if len(bobRows) != 1 || bobRows[0].Scope != ScopeTeam {
		t.Fatalf("teammate List = %+v", bobRows)
	}

	// carol shares no group → invisible.
	carolVis := Visibility{CallerID: "carol"}
	if _, err := s.Get(ctx, "id-team", carolVis); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-teammate Get: want ErrNotFound, got %v", err)
	}
	carolRows, err := s.List(ctx, carolVis, "main", "createOrder")
	if err != nil {
		t.Fatalf("non-teammate List: %v", err)
	}
	if len(carolRows) != 0 {
		t.Fatalf("non-teammate List should be empty, got %+v", carolRows)
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

func TestNormaliseScope(t *testing.T) {
	cases := map[string]string{
		"":         ScopePrivate,
		"private":  ScopePrivate,
		"Private":  ScopePrivate,
		"PRIVATE":  ScopePrivate,
		"team":     ScopeTeam,
		"TEAM":     ScopeTeam,
		"public":   ScopePublic,
		"PUBLIC":   ScopePublic,
	}
	for in, want := range cases {
		got, err := NormaliseScope(in)
		if err != nil {
			t.Errorf("NormaliseScope(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("NormaliseScope(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := NormaliseScope("organisation"); err == nil {
		t.Error("expected error for unknown scope")
	}
}

func TestScopeSharedRoundtrip(t *testing.T) {
	for _, tc := range []struct {
		scope  string
		shared bool
	}{
		{ScopePrivate, false},
		{ScopeTeam, true},
		{ScopePublic, true},
	} {
		if got := SharedFromScope(tc.scope); got != tc.shared {
			t.Errorf("SharedFromScope(%q) = %v, want %v", tc.scope, got, tc.shared)
		}
	}
	if got := ScopeFromShared(true); got != ScopePublic {
		t.Errorf("ScopeFromShared(true) = %q, want PUBLIC", got)
	}
	if got := ScopeFromShared(false); got != ScopePrivate {
		t.Errorf("ScopeFromShared(false) = %q, want PRIVATE", got)
	}
}
