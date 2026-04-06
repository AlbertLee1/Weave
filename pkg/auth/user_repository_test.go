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

func setupUserRepo(t *testing.T) *auth.PGUserRepository {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return auth.NewPGUserRepository(pg.Pool)
}

func TestUserRepository_CreateAndGetByID(t *testing.T) {
	repo := setupUserRepo(t)
	ctx := context.Background()

	u := &auth.UserRecord{
		ID:    "alice",
		Email: "alice@example.com",
		Name:  "Alice",
	}
	if err := repo.CreateUser(ctx, u); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := repo.GetUserByID(ctx, "alice")
	if err != nil {
		t.Fatalf("get by id failed: %v", err)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("expected email alice@example.com, got %q", got.Email)
	}
	if got.Name != "Alice" {
		t.Errorf("expected name Alice, got %q", got.Name)
	}
}

func TestUserRepository_GetByEmail(t *testing.T) {
	repo := setupUserRepo(t)
	ctx := context.Background()

	if err := repo.CreateUser(ctx, &auth.UserRecord{
		ID:    "bob",
		Email: "bob@example.com",
		Name:  "Bob",
	}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	got, err := repo.GetUserByEmail(ctx, "bob@example.com")
	if err != nil {
		t.Fatalf("get by email failed: %v", err)
	}
	if got.ID != "bob" {
		t.Errorf("expected id bob, got %q", got.ID)
	}
}

func TestUserRepository_GetUserByID_NotFound(t *testing.T) {
	repo := setupUserRepo(t)
	_, err := repo.GetUserByID(context.Background(), "nope")
	if !errors.Is(err, auth.ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestUserRepository_UpsertUserRoleAndList(t *testing.T) {
	repo := setupUserRepo(t)
	ctx := context.Background()

	if err := repo.CreateUser(ctx, &auth.UserRecord{ID: "carol"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := repo.UpsertUserRole(ctx, "carol", "admin"); err != nil {
		t.Fatalf("upsert role failed: %v", err)
	}
	// Idempotent
	if err := repo.UpsertUserRole(ctx, "carol", "admin"); err != nil {
		t.Fatalf("upsert role second time failed: %v", err)
	}
	if err := repo.UpsertUserRole(ctx, "carol", "editor"); err != nil {
		t.Fatalf("upsert second role failed: %v", err)
	}

	roles, err := repo.ListUserRoles(ctx, "carol")
	if err != nil {
		t.Fatalf("list roles failed: %v", err)
	}
	if len(roles) != 2 {
		t.Errorf("expected 2 roles, got %d (%v)", len(roles), roles)
	}
}

func TestUserRepository_ListUserOntologyRoles(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	repo := auth.NewPGUserRepository(pg.Pool)
	ctx := context.Background()

	// Seed an ontology so the FK is satisfied
	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)`,
		"ri.ontology.main.ontology.test", "test", "Test"); err != nil {
		t.Fatalf("seed ontology failed: %v", err)
	}

	if err := repo.CreateUser(ctx, &auth.UserRecord{ID: "dan"}); err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO user_ontology_roles (user_id, ontology_rid, role) VALUES ($1, $2, $3)`,
		"dan", "ri.ontology.main.ontology.test", "ontology-owner"); err != nil {
		t.Fatalf("seed grant failed: %v", err)
	}

	scoped, err := repo.ListUserOntologyRoles(ctx, "dan")
	if err != nil {
		t.Fatalf("list scoped failed: %v", err)
	}
	if got := scoped["ri.ontology.main.ontology.test"]; got != "ontology-owner" {
		t.Errorf("expected ontology-owner for test, got %q", got)
	}
}
