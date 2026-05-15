//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
)

// TestVertexScenarioMigration_Given_FreshDB_When_MigrateUp_Then_TablesExist
// covers VTX-001: scenario / scenario_edits 表 Migration.
func TestVertexScenarioMigration_Given_FreshDB_When_MigrateUp_Then_TablesExist(t *testing.T) {
	pg := testutil.StartPGContainer(t)

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	expected := []string{
		"case_studies",
		"scenarios",
		"scenario_edits",
		"scenario_overrides",
	}
	for _, table := range expected {
		if !pg.TableExists(t, table) {
			t.Errorf("expected vertex table %q to exist", table)
		}
	}
}

// TestVertexScenarioMigration_Given_FreshDB_When_MigrateUp_Then_CompositeIndexExists
// verifies the (scenario_rid, object_id) composite index on scenario_edits.
func TestVertexScenarioMigration_Given_FreshDB_When_MigrateUp_Then_CompositeIndexExists(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	var exists bool
	err := pg.Pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_indexes
			WHERE schemaname = 'public'
			  AND tablename = 'scenario_edits'
			  AND indexdef ILIKE '%(scenario_rid, object_id)%'
		)`).Scan(&exists)
	if err != nil {
		t.Fatalf("failed to query pg_indexes: %v", err)
	}
	if !exists {
		t.Error("expected composite index on scenario_edits(scenario_rid, object_id)")
	}
}

// TestVertexScenarioMigration_Given_ScenarioWithEdits_When_DownThenUp_Then_NoResidualRows
// validates that down + up wipes data and no FK violations are left.
func TestVertexScenarioMigration_Given_ScenarioWithEdits_When_DownThenUp_Then_NoResidualRows(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("first up failed: %v", err)
	}

	// Seed: ontology → case_study → scenario → 5 edits
	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)`,
		"ri.ontology.main.ontology.vtx", "vtx", "Vertex"); err != nil {
		t.Fatalf("insert ontology failed: %v", err)
	}
	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO case_studies (rid, name, ontology_rid, created_by) VALUES ($1, $2, $3, $4)`,
		"ri.vertex.main.case-study.cs1", "JFK Ops", "ri.ontology.main.ontology.vtx", "alice"); err != nil {
		t.Fatalf("insert case_study failed: %v", err)
	}
	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO scenarios (rid, case_study_rid, name, parent_ontology_commit, created_by) VALUES ($1, $2, $3, $4, $5)`,
		"ri.vertex.main.scenario.s1", "ri.vertex.main.case-study.cs1", "Snowstorm", "commit-abc", "alice"); err != nil {
		t.Fatalf("insert scenario failed: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := pg.Pool.Exec(ctx,
			`INSERT INTO scenario_edits (scenario_rid, op, object_type, object_id, property, new_value)
			 VALUES ($1, 'modifyProperty', 'Airport', 'JFK', 'capacity', $2::jsonb)`,
			"ri.vertex.main.scenario.s1", "150"); err != nil {
			t.Fatalf("insert edit %d failed: %v", i, err)
		}
	}

	if err := database.RunMigrationsDown(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration down failed: %v", err)
	}
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("second up failed: %v", err)
	}

	for _, table := range []string{"case_studies", "scenarios", "scenario_edits", "scenario_overrides"} {
		var count int
		if err := pg.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count on %s failed: %v", table, err)
		}
		if count != 0 {
			t.Errorf("expected %s to be empty after down+up, got %d rows", table, count)
		}
	}
}

// TestVertexScenarioMigration_Given_Scenarios_When_InspectColumns_Then_HasExpectedColumns
// verifies key columns exist on each new table (catches a partial migration).
func TestVertexScenarioMigration_Given_Scenarios_When_InspectColumns_Then_HasExpectedColumns(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	checks := map[string][]string{
		"case_studies":       {"rid", "name", "ontology_rid", "created_by", "created_at"},
		"scenarios":          {"rid", "case_study_rid", "name", "parent_ontology_commit", "status", "immutable", "created_by", "created_at"},
		"scenario_edits":     {"scenario_rid", "seq", "op", "object_type", "object_id", "property", "new_value", "link_type", "src_id", "dst_id", "created_at"},
		"scenario_overrides": {"scenario_rid", "model_rid", "parameter", "object_id", "value", "applied_at"},
	}
	for table, cols := range checks {
		for _, col := range cols {
			var exists bool
			err := pg.Pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
				)`, table, col).Scan(&exists)
			if err != nil {
				t.Fatalf("column query failed for %s.%s: %v", table, col, err)
			}
			if !exists {
				t.Errorf("expected column %s.%s", table, col)
			}
		}
	}
}
