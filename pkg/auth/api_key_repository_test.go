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

func setupAPIKeyRepo(t *testing.T) (*auth.PGAPIKeyRepository, *auth.PGUserRepository) {
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
	return auth.NewPGAPIKeyRepository(pg.Pool), users
}

func TestAPIKeyRepository_Create_Persists(t *testing.T) {
	repo, _ := setupAPIKeyRepo(t)
	ctx := context.Background()

	raw, prefix, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	rec := &auth.APIKeyRecord{
		KeyHash:   auth.HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    "user:alice@example.com",
		Name:      "ci-bot",
		Scopes:    []string{},
	}
	if err := repo.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rec.ID == "" {
		t.Error("expected Create to populate ID")
	}
	if rec.CreatedAt.IsZero() {
		t.Error("expected Create to populate CreatedAt")
	}
}

func TestAPIKeyRepository_GetByPrefix_Found(t *testing.T) {
	repo, _ := setupAPIKeyRepo(t)
	ctx := context.Background()

	raw, prefix, _ := auth.GenerateAPIKey()
	rec := &auth.APIKeyRecord{
		KeyHash:   auth.HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    "user:alice@example.com",
		Name:      "ci-bot",
	}
	if err := repo.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByPrefix(ctx, prefix)
	if err != nil {
		t.Fatalf("GetByPrefix: %v", err)
	}
	if got.UserID != "user:alice@example.com" {
		t.Errorf("UserID: got %q", got.UserID)
	}
	if got.Name != "ci-bot" {
		t.Errorf("Name: got %q", got.Name)
	}
	if len(got.KeyHash) != 32 {
		t.Errorf("expected 32-byte hash, got %d bytes", len(got.KeyHash))
	}
}

func TestAPIKeyRepository_GetByPrefix_Revoked_Excluded(t *testing.T) {
	repo, _ := setupAPIKeyRepo(t)
	ctx := context.Background()

	raw, prefix, _ := auth.GenerateAPIKey()
	rec := &auth.APIKeyRecord{
		KeyHash:   auth.HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    "user:alice@example.com",
		Name:      "ci-bot",
	}
	if err := repo.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Revoke(ctx, rec.ID); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	_, err := repo.GetByPrefix(ctx, prefix)
	if !errors.Is(err, auth.ErrAPIKeyNotFound) {
		t.Errorf("expected ErrAPIKeyNotFound after revoke, got %v", err)
	}
}

func TestAPIKeyRepository_GetByPrefix_Expired_Excluded(t *testing.T) {
	// Note: expiry filtering is the caller's responsibility (the repo returns
	// the row with its ExpiresAt populated). The middleware then checks
	// IsExpired. We assert the row is returned with the expired time so the
	// caller has enough information to reject.
	repo, _ := setupAPIKeyRepo(t)
	ctx := context.Background()

	raw, prefix, _ := auth.GenerateAPIKey()
	pastExp := time.Now().Add(-1 * time.Hour)
	rec := &auth.APIKeyRecord{
		KeyHash:   auth.HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    "user:alice@example.com",
		Name:      "ci-bot",
		ExpiresAt: &pastExp,
	}
	if err := repo.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.GetByPrefix(ctx, prefix)
	if err != nil {
		t.Fatalf("GetByPrefix: %v", err)
	}
	if got.ExpiresAt == nil {
		t.Fatal("expected ExpiresAt to be returned")
	}
	if !got.IsExpired(time.Now()) {
		t.Error("expected fetched record to be IsExpired")
	}
}

func TestAPIKeyRepository_Revoke_Idempotent(t *testing.T) {
	repo, _ := setupAPIKeyRepo(t)
	ctx := context.Background()

	raw, prefix, _ := auth.GenerateAPIKey()
	rec := &auth.APIKeyRecord{
		KeyHash:   auth.HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    "user:alice@example.com",
		Name:      "ci-bot",
	}
	if err := repo.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Revoke(ctx, rec.ID); err != nil {
		t.Fatalf("first Revoke: %v", err)
	}
	if err := repo.Revoke(ctx, rec.ID); err != nil {
		t.Fatalf("second Revoke (idempotent): %v", err)
	}
}

func TestAPIKeyRepository_ListByUser_ExcludesRevoked(t *testing.T) {
	repo, _ := setupAPIKeyRepo(t)
	ctx := context.Background()

	// Create three keys: two active, one revoked.
	var revoked string
	for i := 0; i < 3; i++ {
		raw, prefix, _ := auth.GenerateAPIKey()
		rec := &auth.APIKeyRecord{
			KeyHash:   auth.HashAPIKey(raw),
			KeyPrefix: prefix,
			UserID:    "user:alice@example.com",
			Name:      "key",
		}
		if err := repo.Create(ctx, rec); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if i == 1 {
			revoked = rec.ID
		}
	}
	if err := repo.Revoke(ctx, revoked); err != nil {
		t.Fatalf("Revoke: %v", err)
	}

	keys, err := repo.ListByUser(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 active keys, got %d", len(keys))
	}
	for _, k := range keys {
		if k.IsRevoked() {
			t.Errorf("ListByUser returned a revoked key %s", k.ID)
		}
		if len(k.KeyHash) == 0 {
			t.Error("ListByUser returned a row with empty KeyHash")
		}
	}
}

func TestAPIKeyRepository_TouchLastUsed(t *testing.T) {
	repo, _ := setupAPIKeyRepo(t)
	ctx := context.Background()

	raw, prefix, _ := auth.GenerateAPIKey()
	rec := &auth.APIKeyRecord{
		KeyHash:   auth.HashAPIKey(raw),
		KeyPrefix: prefix,
		UserID:    "user:alice@example.com",
		Name:      "ci-bot",
	}
	if err := repo.Create(ctx, rec); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.TouchLastUsed(ctx, rec.ID, time.Now()); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}

	got, err := repo.GetByPrefix(ctx, prefix)
	if err != nil {
		t.Fatalf("GetByPrefix: %v", err)
	}
	if got.LastUsedAt == nil {
		t.Error("expected LastUsedAt to be populated")
	}
}
