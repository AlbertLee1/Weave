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

func setupRoleRepo(t *testing.T) *auth.PGRoleRepository {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return auth.NewPGRoleRepository(pg.Pool)
}

func TestRoleRepository_Seed_Builtins(t *testing.T) {
	repo := setupRoleRepo(t)
	ctx := context.Background()

	for _, name := range []string{
		auth.RoleViewer,
		auth.RoleEditor,
		auth.RoleOntologyOwner,
		auth.RoleAdmin,
		auth.RoleIngestWriter,
	} {
		got, err := repo.Get(ctx, name)
		if err != nil {
			t.Errorf("Get(%q): %v", name, err)
			continue
		}
		if !got.Builtin {
			t.Errorf("role %q: expected Builtin=true", name)
		}
	}
}

func TestRoleRepository_CreateCustomRole(t *testing.T) {
	repo := setupRoleRepo(t)
	ctx := context.Background()

	role := &auth.Role{Name: "data-scientist", Description: "ML team"}
	if err := repo.Create(ctx, role); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if role.CreatedAt.IsZero() {
		t.Error("Create did not populate CreatedAt")
	}

	got, err := repo.Get(ctx, "data-scientist")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Builtin {
		t.Error("custom role should have Builtin=false")
	}
}

func TestRoleRepository_Create_Conflict(t *testing.T) {
	repo := setupRoleRepo(t)
	ctx := context.Background()

	err := repo.Create(ctx, &auth.Role{Name: auth.RoleAdmin})
	if !errors.Is(err, auth.ErrRoleConflict) {
		t.Errorf("expected ErrRoleConflict, got %v", err)
	}
}

func TestRoleRepository_List_BuiltinsFirst(t *testing.T) {
	repo := setupRoleRepo(t)
	ctx := context.Background()

	if err := repo.Create(ctx, &auth.Role{Name: "data-scientist"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	list, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	// Built-ins (5) + custom (1) = 6.
	if len(list) != 6 {
		t.Fatalf("expected 6 roles, got %d", len(list))
	}
	// First 5 are built-ins.
	for i := 0; i < 5; i++ {
		if !list[i].Builtin {
			t.Errorf("list[%d] = %q expected builtin", i, list[i].Name)
		}
	}
	if list[5].Name != "data-scientist" || list[5].Builtin {
		t.Errorf("list[5] = %+v, expected custom data-scientist", list[5])
	}
}

func TestRoleRepository_Delete_Custom(t *testing.T) {
	repo := setupRoleRepo(t)
	ctx := context.Background()

	if err := repo.Create(ctx, &auth.Role{Name: "temp"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Delete(ctx, "temp"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, "temp"); !errors.Is(err, auth.ErrRoleNotFound) {
		t.Errorf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestRoleRepository_SetPermissions_ReplaceAndList(t *testing.T) {
	repo := setupRoleRepo(t)
	ctx := context.Background()

	if err := repo.Create(ctx, &auth.Role{Name: "read-only-mission"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Initial set of two perms.
	if err := repo.SetPermissions(ctx, "read-only-mission", []string{
		auth.PermOntologyRead, auth.PermObjectRead,
	}); err != nil {
		t.Fatalf("SetPermissions: %v", err)
	}
	perms, err := repo.ListPermissions(ctx, "read-only-mission")
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(perms) != 2 {
		t.Errorf("expected 2 perms, got %v", perms)
	}

	// Replace with a single perm — the DELETE-then-INSERT inside the tx
	// must strip the old row atomically.
	if err := repo.SetPermissions(ctx, "read-only-mission", []string{
		auth.PermActionExecute,
	}); err != nil {
		t.Fatalf("SetPermissions replace: %v", err)
	}
	perms, _ = repo.ListPermissions(ctx, "read-only-mission")
	if len(perms) != 1 || perms[0] != auth.PermActionExecute {
		t.Errorf("after replace: expected [action.execute], got %v", perms)
	}

	// Empty slice clears everything.
	if err := repo.SetPermissions(ctx, "read-only-mission", nil); err != nil {
		t.Fatalf("SetPermissions clear: %v", err)
	}
	perms, _ = repo.ListPermissions(ctx, "read-only-mission")
	if len(perms) != 0 {
		t.Errorf("after clear: expected empty, got %v", perms)
	}
}

func TestRoleRepository_SetPermissions_UnknownRole(t *testing.T) {
	repo := setupRoleRepo(t)
	ctx := context.Background()

	err := repo.SetPermissions(ctx, "ghost", []string{auth.PermObjectRead})
	if !errors.Is(err, auth.ErrRoleNotFound) {
		t.Errorf("expected ErrRoleNotFound, got %v", err)
	}
}

func TestRoleRepository_UpdateDescription(t *testing.T) {
	repo := setupRoleRepo(t)
	ctx := context.Background()

	got, err := repo.UpdateDescription(ctx, auth.RoleViewer, "updated viewer desc")
	if err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}
	if got.Description != "updated viewer desc" {
		t.Errorf("got %q", got.Description)
	}
}
