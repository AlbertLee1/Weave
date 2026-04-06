//go:build integration

package auth_test

import (
	"context"
	"slices"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
)

func setupBootstrapRepo(t *testing.T) *auth.PGUserRepository {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return auth.NewPGUserRepository(pg.Pool)
}

func TestBootstrapAdmin_CreatesUserAndGrantsAdmin(t *testing.T) {
	repo := setupBootstrapRepo(t)
	ctx := context.Background()

	if err := auth.BootstrapAdmin(ctx, repo, "alice@example.com"); err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}

	got, err := repo.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("user not created: %v", err)
	}
	roles, err := repo.ListUserRoles(ctx, got.ID)
	if err != nil {
		t.Fatalf("list roles failed: %v", err)
	}
	if !slices.Contains(roles, "admin") {
		t.Errorf("expected admin role, got %v", roles)
	}
}

func TestBootstrapAdmin_Idempotent(t *testing.T) {
	repo := setupBootstrapRepo(t)
	ctx := context.Background()

	if err := auth.BootstrapAdmin(ctx, repo, "alice@example.com"); err != nil {
		t.Fatalf("first bootstrap failed: %v", err)
	}
	if err := auth.BootstrapAdmin(ctx, repo, "alice@example.com"); err != nil {
		t.Fatalf("second bootstrap should be a no-op, got error: %v", err)
	}

	got, err := repo.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("user not present: %v", err)
	}
	roles, err := repo.ListUserRoles(ctx, got.ID)
	if err != nil {
		t.Fatalf("list roles failed: %v", err)
	}
	adminCount := 0
	for _, r := range roles {
		if r == "admin" {
			adminCount++
		}
	}
	if adminCount != 1 {
		t.Errorf("expected exactly 1 admin grant, got %d", adminCount)
	}
}

func TestBootstrapAdmin_EmptyEmailIsNoOp(t *testing.T) {
	repo := setupBootstrapRepo(t)
	if err := auth.BootstrapAdmin(context.Background(), repo, ""); err != nil {
		t.Errorf("empty email should be a no-op, got error: %v", err)
	}
}
