//go:build integration

package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
)

func setupGroupRepo(t *testing.T) (*auth.PGGroupRepository, func(string)) {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	users := auth.NewPGUserRepository(pg.Pool)
	seed := func(id string) {
		if err := users.CreateUser(context.Background(), &auth.UserRecord{
			ID: id, Email: id, Name: id,
		}); err != nil {
			t.Fatalf("seed user %s: %v", id, err)
		}
	}
	return auth.NewPGGroupRepository(pg.Pool), seed
}

func TestGroupRepository_CRUD(t *testing.T) {
	repo, _ := setupGroupRepo(t)
	ctx := context.Background()

	g := &auth.Group{Name: "analysts", Description: "na analysts team"}
	if err := repo.Create(ctx, g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if g.ID == "" || g.CreatedAt.IsZero() {
		t.Error("Create did not populate ID/CreatedAt")
	}

	got, err := repo.GetByID(ctx, g.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != g.Name || got.Description != g.Description {
		t.Errorf("round-trip mismatch: got %+v", got)
	}

	byName, err := repo.GetByName(ctx, "analysts")
	if err != nil || byName.ID != g.ID {
		t.Errorf("GetByName: %v / %v", err, byName)
	}

	// Update: patch description only; name preserved.
	newDesc := "updated desc"
	upd, err := repo.Update(ctx, g.ID, auth.GroupUpdate{Description: &newDesc})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if upd.Description != "updated desc" || upd.Name != "analysts" {
		t.Errorf("Update: got %+v", upd)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("List: expected 1, got %d", len(list))
	}

	if err := repo.Delete(ctx, g.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, g.ID); !errors.Is(err, auth.ErrGroupNotFound) {
		t.Errorf("after Delete expected ErrGroupNotFound, got %v", err)
	}
}

func TestGroupRepository_NameConflict(t *testing.T) {
	repo, _ := setupGroupRepo(t)
	ctx := context.Background()

	if err := repo.Create(ctx, &auth.Group{Name: "analysts"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	err := repo.Create(ctx, &auth.Group{Name: "analysts"})
	if !errors.Is(err, auth.ErrGroupNameConflict) {
		t.Errorf("expected ErrGroupNameConflict, got %v", err)
	}
}

func TestGroupRepository_Membership(t *testing.T) {
	repo, seed := setupGroupRepo(t)
	ctx := context.Background()

	seed("user:alice@example.com")
	seed("user:bob@example.com")

	g := &auth.Group{Name: "analysts"}
	if err := repo.Create(ctx, g); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.AddMember(ctx, g.ID, "user:alice@example.com"); err != nil {
		t.Fatalf("AddMember alice: %v", err)
	}
	if err := repo.AddMember(ctx, g.ID, "user:bob@example.com"); err != nil {
		t.Fatalf("AddMember bob: %v", err)
	}
	// Idempotent: re-adding alice is a no-op, not an error.
	if err := repo.AddMember(ctx, g.ID, "user:alice@example.com"); err != nil {
		t.Fatalf("AddMember alice (dupe): %v", err)
	}

	members, err := repo.ListMembers(ctx, g.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d: %v", len(members), members)
	}

	userGroups, err := repo.ListUserGroups(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("ListUserGroups: %v", err)
	}
	if len(userGroups) != 1 || userGroups[0] != g.ID {
		t.Errorf("ListUserGroups alice: got %v", userGroups)
	}

	if err := repo.RemoveMember(ctx, g.ID, "user:alice@example.com"); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
	// Idempotent: removing again is not an error.
	if err := repo.RemoveMember(ctx, g.ID, "user:alice@example.com"); err != nil {
		t.Fatalf("RemoveMember (dupe): %v", err)
	}

	members, _ = repo.ListMembers(ctx, g.ID)
	if len(members) != 1 {
		t.Errorf("after remove: expected 1 member, got %v", members)
	}
}

func TestGroupRepository_CascadeOnDelete(t *testing.T) {
	repo, seed := setupGroupRepo(t)
	ctx := context.Background()

	seed("user:alice@example.com")
	g := &auth.Group{Name: "analysts"}
	if err := repo.Create(ctx, g); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.AddMember(ctx, g.ID, "user:alice@example.com"); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := repo.Delete(ctx, g.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// Cascade: alice no longer belongs to any groups.
	groups, _ := repo.ListUserGroups(ctx, "user:alice@example.com")
	if len(groups) != 0 {
		t.Errorf("expected membership cascade on Delete, got %v", groups)
	}
}
