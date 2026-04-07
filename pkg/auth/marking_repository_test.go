//go:build integration

package auth_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
)

// setupMarkingRepo spins up a fresh PostgreSQL container with all
// migrations applied, plus a single seed user that the grant tests can
// reference. The marking seed rows come from migration 000012, so the
// markings table is already pre-populated when the helper returns.
func setupMarkingRepo(t *testing.T) (*auth.PGMarkingRepository, *auth.PGUserRepository) {
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
	return auth.NewPGMarkingRepository(pg.Pool), users
}

// TestMarkingRepository_ListMarkings_ReturnsSeeded asserts that the five
// canonical markings inserted by 000012_markings.up.sql are visible to
// callers immediately after migration. This is the smoke test for the
// migration itself: if the seed insert is dropped or the table renamed,
// this test fails on day one.
func TestMarkingRepository_ListMarkings_ReturnsSeeded(t *testing.T) {
	repo, _ := setupMarkingRepo(t)
	ctx := context.Background()

	got, err := repo.ListMarkings(ctx)
	if err != nil {
		t.Fatalf("ListMarkings: %v", err)
	}
	want := map[string]bool{
		"PUBLIC":       false,
		"INTERNAL":     false,
		"CONFIDENTIAL": false,
		"PII":          false,
		"SECRET":       false,
	}
	for _, m := range got {
		if _, ok := want[m.Name]; ok {
			want[m.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("expected seeded marking %q to be returned by ListMarkings", name)
		}
	}
}

// TestMarkingRepository_GrantMarking_Persists creates a single grant and
// verifies it shows up in GetUserMarkings. This is the round-trip happy
// path.
func TestMarkingRepository_GrantMarking_Persists(t *testing.T) {
	repo, _ := setupMarkingRepo(t)
	ctx := context.Background()

	if err := repo.GrantMarking(ctx, "user:alice@example.com", "PII", "user:admin"); err != nil {
		t.Fatalf("GrantMarking: %v", err)
	}

	names, err := repo.GetUserMarkings(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("GetUserMarkings: %v", err)
	}
	found := false
	for _, n := range names {
		if n == "PII" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected PII grant in user markings, got %v", names)
	}
}

// TestMarkingRepository_GrantMarking_Idempotent verifies that calling
// GrantMarking twice for the same (user, marking) pair does not error and
// does not produce duplicate rows. Admin tooling will hit this path
// constantly so we don't want unique-constraint errors leaking out.
func TestMarkingRepository_GrantMarking_Idempotent(t *testing.T) {
	repo, _ := setupMarkingRepo(t)
	ctx := context.Background()

	if err := repo.GrantMarking(ctx, "user:alice@example.com", "PII", "user:admin"); err != nil {
		t.Fatalf("first GrantMarking: %v", err)
	}
	if err := repo.GrantMarking(ctx, "user:alice@example.com", "PII", "user:admin"); err != nil {
		t.Fatalf("second GrantMarking should be idempotent: %v", err)
	}

	names, err := repo.GetUserMarkings(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("GetUserMarkings: %v", err)
	}
	count := 0
	for _, n := range names {
		if n == "PII" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 PII grant after idempotent re-grant, got %d", count)
	}
}

// TestMarkingRepository_RevokeMarking removes an existing grant and
// verifies it no longer appears. Re-revoking is also expected to be a
// no-op (no error) so admin double-clicks are safe.
func TestMarkingRepository_RevokeMarking(t *testing.T) {
	repo, _ := setupMarkingRepo(t)
	ctx := context.Background()

	if err := repo.GrantMarking(ctx, "user:alice@example.com", "PII", "user:admin"); err != nil {
		t.Fatalf("GrantMarking: %v", err)
	}
	if err := repo.RevokeMarking(ctx, "user:alice@example.com", "PII"); err != nil {
		t.Fatalf("RevokeMarking: %v", err)
	}

	names, err := repo.GetUserMarkings(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("GetUserMarkings: %v", err)
	}
	for _, n := range names {
		if n == "PII" {
			t.Errorf("expected PII grant to be gone after revoke, still present in %v", names)
		}
	}

	// Idempotent re-revoke.
	if err := repo.RevokeMarking(ctx, "user:alice@example.com", "PII"); err != nil {
		t.Errorf("re-revoke should be idempotent, got %v", err)
	}
}

// TestMarkingRepository_GetUserMarkings_Empty verifies the empty path:
// a user with no grants returns an empty (non-nil) slice. This is the
// shape the MarkingFilter expects so it can range without nil checks.
func TestMarkingRepository_GetUserMarkings_Empty(t *testing.T) {
	repo, _ := setupMarkingRepo(t)
	ctx := context.Background()

	names, err := repo.GetUserMarkings(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("GetUserMarkings: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("expected empty grant list for fresh user, got %v", names)
	}
}

// TestMarkingRepository_GetUserMarkings_Multiple grants three markings
// and verifies the returned slice contains exactly those three names.
// Order is not asserted because the SQL does not enforce one.
func TestMarkingRepository_GetUserMarkings_Multiple(t *testing.T) {
	repo, _ := setupMarkingRepo(t)
	ctx := context.Background()

	grants := []string{"PUBLIC", "INTERNAL", "CONFIDENTIAL"}
	for _, m := range grants {
		if err := repo.GrantMarking(ctx, "user:alice@example.com", m, "user:admin"); err != nil {
			t.Fatalf("GrantMarking %s: %v", m, err)
		}
	}

	got, err := repo.GetUserMarkings(ctx, "user:alice@example.com")
	if err != nil {
		t.Fatalf("GetUserMarkings: %v", err)
	}
	if len(got) != len(grants) {
		t.Fatalf("expected %d grants, got %d (%v)", len(grants), len(got), got)
	}
	gotSet := map[string]bool{}
	for _, n := range got {
		gotSet[n] = true
	}
	for _, want := range grants {
		if !gotSet[want] {
			t.Errorf("expected grant %q to be present, got %v", want, got)
		}
	}
}
