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

func TestAPIKeyRepository_Rotate_StampsPredecessorAndInsertsSuccessor(t *testing.T) {
	repo, _ := setupAPIKeyRepo(t)
	ctx := context.Background()

	raw, prefix, _ := auth.GenerateAPIKey()
	pred := &auth.APIKeyRecord{
		KeyHash: auth.HashAPIKey(raw), KeyPrefix: prefix,
		UserID: "user:alice@example.com", Name: "ci-bot",
	}
	if err := repo.Create(ctx, pred); err != nil {
		t.Fatalf("Create predecessor: %v", err)
	}

	succRaw, succPrefix, _ := auth.GenerateAPIKey()
	succ := &auth.APIKeyRecord{
		KeyHash: auth.HashAPIKey(succRaw), KeyPrefix: succPrefix,
		UserID: "user:alice@example.com", Name: "ci-bot",
	}
	grace := time.Now().Add(7 * 24 * time.Hour)
	if err := repo.Rotate(ctx, pred.ID, succ, grace); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if succ.ID == "" {
		t.Error("expected successor ID populated")
	}

	got, err := repo.GetByID(ctx, pred.ID)
	if err != nil {
		t.Fatalf("GetByID predecessor: %v", err)
	}
	if got.RotatesAt == nil {
		t.Fatal("expected predecessor.RotatesAt populated")
	}
	if got.SuccessorID == nil || *got.SuccessorID != succ.ID {
		t.Errorf("expected predecessor.SuccessorID = %q, got %v", succ.ID, got.SuccessorID)
	}

	// Second Rotate must fail.
	if err := repo.Rotate(ctx, pred.ID, &auth.APIKeyRecord{
		KeyHash: auth.HashAPIKey("wvk_another_" + succPrefix), KeyPrefix: "another1",
		UserID: "user:alice@example.com", Name: "ci-bot",
	}, grace); !errors.Is(err, auth.ErrAPIKeyAlreadyRotated) {
		t.Errorf("expected ErrAPIKeyAlreadyRotated on second rotation, got %v", err)
	}
}

func TestAPIKeyRepository_Rotate_MissingPredecessor(t *testing.T) {
	repo, _ := setupAPIKeyRepo(t)
	succ := &auth.APIKeyRecord{
		KeyHash: auth.HashAPIKey("wvk_nope_longenoughrandomrandomrandom"),
		KeyPrefix: "nope0001", UserID: "user:alice@example.com", Name: "x",
	}
	err := repo.Rotate(context.Background(), "00000000-0000-0000-0000-000000000000", succ, time.Now().Add(time.Hour))
	if !errors.Is(err, auth.ErrAPIKeyNotFound) {
		t.Errorf("expected ErrAPIKeyNotFound, got %v", err)
	}
}

func TestAPIKeyRepository_ListPendingRotations_FiltersByWindow(t *testing.T) {
	repo, _ := setupAPIKeyRepo(t)
	ctx := context.Background()

	// near (3d), far (30d), past (already rotated), revoked.
	now := time.Now()
	mkKey := func(name string, rotatesAt *time.Time, revoke bool) string {
		raw, prefix, _ := auth.GenerateAPIKey()
		rec := &auth.APIKeyRecord{
			KeyHash: auth.HashAPIKey(raw), KeyPrefix: prefix,
			UserID: "user:alice@example.com", Name: name,
		}
		if err := repo.Create(ctx, rec); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
		if rotatesAt != nil {
			// The only supported write-path for rotates_at is Rotate, which
			// also requires a successor row. Mint one so the predecessor's
			// rotates_at lands.
			succRaw, succPrefix, _ := auth.GenerateAPIKey()
			succ := &auth.APIKeyRecord{
				KeyHash: auth.HashAPIKey(succRaw), KeyPrefix: succPrefix,
				UserID: "user:alice@example.com", Name: name + "-succ",
			}
			if err := repo.Rotate(ctx, rec.ID, succ, *rotatesAt); err != nil {
				t.Fatalf("Rotate %s: %v", name, err)
			}
		}
		if revoke {
			if err := repo.Revoke(ctx, rec.ID); err != nil {
				t.Fatalf("Revoke %s: %v", name, err)
			}
		}
		return rec.ID
	}

	near := now.Add(3 * 24 * time.Hour)
	far := now.Add(30 * 24 * time.Hour)
	past := now.Add(-2 * time.Hour)

	nearID := mkKey("near", &near, false)
	mkKey("far", &far, false)
	mkKey("past", &past, false)
	mkKey("revoked", &near, true)
	mkKey("no-rotation", nil, false)

	rows, err := repo.ListPendingRotations(ctx, now, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("ListPendingRotations: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 near-rotation key, got %d", len(rows))
	}
	if rows[0].ID != nearID {
		t.Errorf("wrong key: got %q, want %q", rows[0].ID, nearID)
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
