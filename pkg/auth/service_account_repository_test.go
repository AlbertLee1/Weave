//go:build integration

package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
)

func setupServiceAccountRepo(t *testing.T) *auth.PGServiceAccountRepository {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	users := auth.NewPGUserRepository(pg.Pool)
	if err := users.CreateUser(context.Background(), &auth.UserRecord{
		ID:    "user:alice@example.com",
		Email: "alice@example.com",
		Name:  "Alice",
	}); err != nil {
		t.Fatalf("seed user failed: %v", err)
	}
	return auth.NewPGServiceAccountRepository(pg.Pool)
}

func TestServiceAccountRepository_Create_Persists(t *testing.T) {
	repo := setupServiceAccountRepo(t)
	ctx := context.Background()

	sa := &auth.ServiceAccount{
		Name:        "ci-bot",
		Description: "GitHub Actions",
		OwnerUserID: "user:alice@example.com",
		Scopes:      []string{"read:objects"},
	}
	if err := repo.Create(ctx, sa); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sa.ID == "" {
		t.Error("expected Create to populate ID")
	}
	if sa.CreatedAt.IsZero() {
		t.Error("expected Create to populate CreatedAt")
	}

	got, err := repo.GetByID(ctx, sa.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "ci-bot" {
		t.Errorf("Name: got %q", got.Name)
	}
	if got.Description != "GitHub Actions" {
		t.Errorf("Description: got %q", got.Description)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != "read:objects" {
		t.Errorf("Scopes: got %v", got.Scopes)
	}
}

func TestServiceAccountRepository_Create_NameConflict(t *testing.T) {
	repo := setupServiceAccountRepo(t)
	ctx := context.Background()

	sa1 := &auth.ServiceAccount{Name: "ci-bot", OwnerUserID: "user:alice@example.com"}
	if err := repo.Create(ctx, sa1); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	sa2 := &auth.ServiceAccount{Name: "ci-bot", OwnerUserID: "user:alice@example.com"}
	err := repo.Create(ctx, sa2)
	if !errors.Is(err, auth.ErrServiceAccountNameConflict) {
		t.Errorf("expected ErrServiceAccountNameConflict, got %v", err)
	}
}

func TestServiceAccountRepository_DisabledAllowsNameReuse(t *testing.T) {
	repo := setupServiceAccountRepo(t)
	ctx := context.Background()

	sa1 := &auth.ServiceAccount{Name: "ci-bot", OwnerUserID: "user:alice@example.com"}
	if err := repo.Create(ctx, sa1); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if err := repo.Disable(ctx, sa1.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	sa2 := &auth.ServiceAccount{Name: "ci-bot", OwnerUserID: "user:alice@example.com"}
	if err := repo.Create(ctx, sa2); err != nil {
		t.Fatalf("recreate after disable: %v", err)
	}

	// Lookup by name returns the active row only.
	got, err := repo.GetByName(ctx, "ci-bot")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.ID != sa2.ID {
		t.Errorf("GetByName returned stale row: got %q want %q", got.ID, sa2.ID)
	}
}

func TestServiceAccountRepository_ListActive_ExcludesDisabled(t *testing.T) {
	repo := setupServiceAccountRepo(t)
	ctx := context.Background()

	var disabledID string
	for i, name := range []string{"a", "b", "c"} {
		sa := &auth.ServiceAccount{Name: name, OwnerUserID: "user:alice@example.com"}
		if err := repo.Create(ctx, sa); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if i == 1 {
			disabledID = sa.ID
		}
	}
	if err := repo.Disable(ctx, disabledID); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	got, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 active, got %d", len(got))
	}
	for _, sa := range got {
		if sa.IsDisabled() {
			t.Errorf("ListActive returned disabled row %s", sa.ID)
		}
	}
}

func TestServiceAccountRepository_Update_PartialPatch(t *testing.T) {
	repo := setupServiceAccountRepo(t)
	ctx := context.Background()

	exp := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	sa := &auth.ServiceAccount{
		Name:        "ci-bot",
		Description: "initial",
		OwnerUserID: "user:alice@example.com",
		Scopes:      []string{"read:objects"},
		ExpiresAt:   &exp,
	}
	if err := repo.Create(ctx, sa); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Patch only description — scopes + expires_at must be preserved.
	newDesc := "updated"
	got, err := repo.Update(ctx, sa.ID, auth.ServiceAccountUpdate{
		Description: &newDesc,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Description != "updated" {
		t.Errorf("Description not updated: got %q", got.Description)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != "read:objects" {
		t.Errorf("Scopes clobbered: got %v", got.Scopes)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt clobbered: got %v", got.ExpiresAt)
	}

	// Clear expires_at via pointer-to-nil-time.
	var clear *time.Time
	got2, err := repo.Update(ctx, sa.ID, auth.ServiceAccountUpdate{
		ExpiresAt: &clear,
	})
	if err != nil {
		t.Fatalf("Update clear: %v", err)
	}
	if got2.ExpiresAt != nil {
		t.Errorf("expected ExpiresAt cleared, got %v", got2.ExpiresAt)
	}
}

func TestServiceAccountRepository_Update_NotFound(t *testing.T) {
	repo := setupServiceAccountRepo(t)
	ctx := context.Background()

	newDesc := "x"
	_, err := repo.Update(ctx, "00000000-0000-0000-0000-000000000000", auth.ServiceAccountUpdate{
		Description: &newDesc,
	})
	if !errors.Is(err, auth.ErrServiceAccountNotFound) {
		t.Errorf("expected ErrServiceAccountNotFound, got %v", err)
	}
}

func TestServiceAccountRepository_Disable_Idempotent(t *testing.T) {
	repo := setupServiceAccountRepo(t)
	ctx := context.Background()

	sa := &auth.ServiceAccount{Name: "ci-bot", OwnerUserID: "user:alice@example.com"}
	if err := repo.Create(ctx, sa); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Disable(ctx, sa.ID); err != nil {
		t.Fatalf("first Disable: %v", err)
	}
	if err := repo.Disable(ctx, sa.ID); err != nil {
		t.Fatalf("second Disable (idempotent): %v", err)
	}
	got, err := repo.GetByID(ctx, sa.ID)
	if err != nil {
		t.Fatalf("GetByID after Disable: %v", err)
	}
	if !got.IsDisabled() {
		t.Error("expected DisabledAt populated")
	}
}
