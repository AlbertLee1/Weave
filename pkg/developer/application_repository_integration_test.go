//go:build integration

package developer_test

import (
	"context"
	"crypto/subtle"
	"errors"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/developer"
)

func setupApplicationRepo(t *testing.T) *developer.PGApplicationRepository {
	t.Helper()
	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration: %v", err)
	}
	return developer.NewPGApplicationRepository(pg.Pool)
}

func mustFreshApp(t *testing.T, owner string) (*developer.Application, string) {
	t.Helper()
	cid, err := developer.GenerateClientID()
	if err != nil {
		t.Fatalf("GenerateClientID: %v", err)
	}
	sec, err := developer.GenerateClientSecret()
	if err != nil {
		t.Fatalf("GenerateClientSecret: %v", err)
	}
	return &developer.Application{
		Name:             "integration-app",
		Description:      "integration",
		ClientID:         cid,
		ClientSecretHash: developer.HashClientSecret(sec),
		RedirectURIs:     []string{"https://example.com/cb"},
		Scopes:           []string{"read:objects"},
		CreatedBy:        owner,
	}, sec
}

func TestApplicationRepository_CreateListGetDelete(t *testing.T) {
	repo := setupApplicationRepo(t)
	ctx := context.Background()

	app, secret := mustFreshApp(t, "user:alice@example.com")
	if err := repo.Create(ctx, app); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if app.ID == "" {
		t.Error("expected ID to be populated by Create")
	}
	if app.CreatedAt.IsZero() || app.UpdatedAt.IsZero() {
		t.Error("expected CreatedAt / UpdatedAt to be populated")
	}

	got, err := repo.GetByID(ctx, app.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "integration-app" {
		t.Errorf("Name: %q", got.Name)
	}
	if subtle.ConstantTimeCompare(got.ClientSecretHash, developer.HashClientSecret(secret)) != 1 {
		t.Error("round-tripped secret hash does not match")
	}
	if len(got.RedirectURIs) != 1 || got.RedirectURIs[0] != "https://example.com/cb" {
		t.Errorf("RedirectURIs: %v", got.RedirectURIs)
	}
	if len(got.Scopes) != 1 || got.Scopes[0] != "read:objects" {
		t.Errorf("Scopes: %v", got.Scopes)
	}

	byClientID, err := repo.GetByClientID(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("GetByClientID: %v", err)
	}
	if byClientID.ID != app.ID {
		t.Errorf("GetByClientID returned wrong row: got %q want %q", byClientID.ID, app.ID)
	}

	list, err := repo.ListByUser(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(list) != 1 || list[0].ID != app.ID {
		t.Fatalf("ListByUser: expected 1 matching row, got %d", len(list))
	}

	// A different user must not see alice's row.
	bobList, err := repo.ListByUser(ctx, "user:bob@example.com")
	if err != nil {
		t.Fatalf("ListByUser bob: %v", err)
	}
	if len(bobList) != 0 {
		t.Errorf("expected bob to see 0 rows, got %d", len(bobList))
	}

	if err := repo.Delete(ctx, app.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, app.ID); !errors.Is(err, developer.ErrApplicationNotFound) {
		t.Errorf("expected ErrApplicationNotFound after delete, got %v", err)
	}
	if err := repo.Delete(ctx, app.ID); !errors.Is(err, developer.ErrApplicationNotFound) {
		t.Errorf("expected ErrApplicationNotFound on second delete, got %v", err)
	}
}

func TestApplicationRepository_ClientIDUnique(t *testing.T) {
	repo := setupApplicationRepo(t)
	ctx := context.Background()

	app1, _ := mustFreshApp(t, "user:alice@example.com")
	if err := repo.Create(ctx, app1); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	// Reuse the same client_id on a second insert — the UNIQUE constraint
	// must reject it.
	app2, _ := mustFreshApp(t, "user:bob@example.com")
	app2.ClientID = app1.ClientID
	if err := repo.Create(ctx, app2); err == nil {
		t.Fatal("expected duplicate client_id insert to fail")
	}
}
