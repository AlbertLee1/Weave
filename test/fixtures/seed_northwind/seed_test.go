//go:build integration

package seed_northwind_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/oms"
	seed "github.com/liyang/weave/test/fixtures/seed_northwind"
)

// TestSeed_IdempotentAndComplete is the US-030 acceptance test: calling
// Seed() twice against a clean PG must produce the same final state
// (wipe-and-reseed semantics), the northwind ontology must carry the
// expected object types with property + history rows ready for the index
// rebuild endpoint, and the three baseline test users must exist with
// global roles granted.
func TestSeed_IdempotentAndComplete(t *testing.T) {
	ctx := context.Background()

	pg := testutil.StartPGContainer(t)
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	opts := seed.DefaultOptions()
	// Keep the test quiet.
	opts.Logger = nil

	// --- First seed pass --------------------------------------------------
	first, err := seed.Seed(ctx, pg.Pool, opts)
	if err != nil {
		t.Fatalf("first Seed: %v", err)
	}
	if first.OntologyAPIName != "northwind" {
		t.Fatalf("ontology apiName = %q, want northwind", first.OntologyAPIName)
	}
	if first.OntologyRID == "" {
		t.Fatalf("ontology RID is empty")
	}
	if len(first.ObjectTypes) == 0 {
		t.Fatalf("no object types returned from first seed")
	}
	// Every seeded object type must carry at least one history row, otherwise
	// the index rebuild endpoint has no data to replay.
	firstCounts := historyCountsByType(ctx, t, pg)
	for _, ot := range first.ObjectTypes {
		if firstCounts[ot] == 0 {
			t.Errorf("object type %q has 0 history rows after first seed", ot)
		}
	}

	// Users must exist with global roles granted.
	repo := auth.NewPGUserRepository(pg.Pool)
	for _, u := range opts.TestUsers {
		rec, err := repo.GetUserByEmail(ctx, u.Email)
		if err != nil {
			t.Fatalf("GetUserByEmail(%q): %v", u.Email, err)
		}
		if rec.PasswordHash == "" {
			t.Errorf("user %q missing password hash", u.Email)
		}
		roles, err := repo.ListUserRoles(ctx, rec.ID)
		if err != nil {
			t.Fatalf("ListUserRoles(%q): %v", rec.ID, err)
		}
		if len(roles) == 0 {
			t.Errorf("user %q has no global roles granted", u.Email)
		}
	}

	// --- Second seed pass (idempotent) -----------------------------------
	second, err := seed.Seed(ctx, pg.Pool, opts)
	if err != nil {
		t.Fatalf("second Seed: %v", err)
	}
	if second.OntologyAPIName != first.OntologyAPIName {
		t.Fatalf("ontology apiName drift: %q vs %q", second.OntologyAPIName, first.OntologyAPIName)
	}
	if len(second.ObjectTypes) != len(first.ObjectTypes) {
		t.Fatalf("object type count drift: %d vs %d", len(second.ObjectTypes), len(first.ObjectTypes))
	}

	// Object type counts per row in object_types must match first pass
	// exactly (wipe-and-reseed, not append).
	otCount := countObjectTypesForOntology(ctx, t, pg, second.OntologyRID)
	if otCount != len(second.ObjectTypes) {
		t.Errorf("object_types row count = %d, want %d", otCount, len(second.ObjectTypes))
	}

	secondCounts := historyCountsByType(ctx, t, pg)
	for _, ot := range second.ObjectTypes {
		if secondCounts[ot] != firstCounts[ot] {
			t.Errorf("history rows for %q drifted: first=%d second=%d",
				ot, firstCounts[ot], secondCounts[ot])
		}
	}

	// Users must still exist exactly once (wipe + reseed removes the
	// previous rows first, so uniqueness constraints hold).
	for _, u := range opts.TestUsers {
		if _, err := repo.GetUserByEmail(ctx, u.Email); err != nil {
			t.Errorf("user %q missing after second seed: %v", u.Email, err)
		}
	}

	// Sanity: oms.PGRepository sees the expected schema after the second
	// pass — it's the same call chain the index rebuild endpoint uses.
	omsRepo := oms.NewPGRepository(pg.Pool)
	for _, otAPI := range second.ObjectTypes {
		ot, err := omsRepo.GetObjectTypeByAPIName(ctx, second.OntologyRID, otAPI)
		if err != nil {
			t.Errorf("GetObjectTypeByAPIName(%q): %v", otAPI, err)
			continue
		}
		if len(ot.Properties) == 0 {
			t.Errorf("object type %q has zero properties", otAPI)
		}
	}
}

func historyCountsByType(ctx context.Context, t *testing.T, pg *testutil.PGContainer) map[string]int {
	t.Helper()
	rows, err := pg.Pool.Query(ctx, `
		SELECT ot.api_name, COUNT(*)
		FROM object_history oh
		JOIN object_types ot ON ot.rid = oh.object_type_rid
		GROUP BY ot.api_name`)
	if err != nil {
		t.Fatalf("count history rows: %v", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			t.Fatalf("scan history count: %v", err)
		}
		out[name] = n
	}
	return out
}

func countObjectTypesForOntology(ctx context.Context, t *testing.T, pg *testutil.PGContainer, ontologyRID string) int {
	t.Helper()
	var n int
	if err := pg.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM object_types WHERE ontology_rid = $1`, ontologyRID).Scan(&n); err != nil {
		t.Fatalf("count object_types: %v", err)
	}
	return n
}
