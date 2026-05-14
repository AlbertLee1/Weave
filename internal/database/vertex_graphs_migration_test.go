//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
)

// TestVertexGraphsMigration_Given_FreshDB_When_MigrateUp_Then_TablesExist
// covers VTX-007: SystemGraph data model migration.
func TestVertexGraphsMigration_Given_FreshDB_When_MigrateUp_Then_TablesExist(t *testing.T) {
	pg := testutil.StartPGContainer(t)

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	expected := []string{"system_graphs", "system_graph_versions"}
	for _, table := range expected {
		if !pg.TableExists(t, table) {
			t.Errorf("expected vertex graph table %q to exist", table)
		}
	}
}

// TestVertexGraphsMigration_Given_FreshDB_When_MigrateUp_Then_HasExpectedColumns
// verifies key columns exist on each new table.
func TestVertexGraphsMigration_Given_FreshDB_When_MigrateUp_Then_HasExpectedColumns(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	checks := map[string][]string{
		"system_graphs":         {"rid", "ontology_rid", "name", "version", "versioned", "payload", "created_by", "created_at", "updated_at"},
		"system_graph_versions": {"graph_rid", "version", "payload", "created_at"},
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

// TestVertexGraphsMigration_Given_VersionedGraph_When_Update_Then_HistoryRowInserted
// verifies that UPDATEs on system_graphs with versioned=true write a row to
// system_graph_versions (auto-history trigger).
func TestVertexGraphsMigration_Given_VersionedGraph_When_Update_Then_HistoryRowInserted(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	seedOntology(t, pg)

	// Insert a versioned graph.
	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO system_graphs (rid, ontology_rid, name, version, versioned, payload, created_by)
		 VALUES ($1, $2, $3, 1, TRUE, $4::jsonb, $5)`,
		"ri.vertex.main.graph.g1", "ri.ontology.main.ontology.vtx", "JFK Map",
		`{"layers":[],"edges":[]}`, "alice"); err != nil {
		t.Fatalf("insert system_graphs failed: %v", err)
	}

	// Update payload + bump version: trigger should write a history row.
	if _, err := pg.Pool.Exec(ctx,
		`UPDATE system_graphs SET payload = $1::jsonb, version = 2, updated_at = NOW()
		 WHERE rid = $2`,
		`{"layers":[{"id":"L1"}],"edges":[]}`, "ri.vertex.main.graph.g1"); err != nil {
		t.Fatalf("update system_graphs failed: %v", err)
	}

	var count int
	if err := pg.Pool.QueryRow(ctx,
		`SELECT count(*) FROM system_graph_versions WHERE graph_rid = $1`,
		"ri.vertex.main.graph.g1").Scan(&count); err != nil {
		t.Fatalf("count history failed: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 history row after versioned update, got %d", count)
	}
}

// TestVertexGraphsMigration_Given_NonVersionedGraph_When_Update_Then_NoHistoryRow
// verifies that UPDATEs with versioned=false do NOT write a history row.
func TestVertexGraphsMigration_Given_NonVersionedGraph_When_Update_Then_NoHistoryRow(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	seedOntology(t, pg)

	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO system_graphs (rid, ontology_rid, name, version, versioned, payload, created_by)
		 VALUES ($1, $2, $3, 1, FALSE, $4::jsonb, $5)`,
		"ri.vertex.main.graph.g2", "ri.ontology.main.ontology.vtx", "Ephemeral",
		`{"layers":[],"edges":[]}`, "alice"); err != nil {
		t.Fatalf("insert system_graphs failed: %v", err)
	}

	if _, err := pg.Pool.Exec(ctx,
		`UPDATE system_graphs SET payload = $1::jsonb, updated_at = NOW() WHERE rid = $2`,
		`{"layers":[{"id":"L1"}],"edges":[]}`, "ri.vertex.main.graph.g2"); err != nil {
		t.Fatalf("update system_graphs failed: %v", err)
	}

	var count int
	if err := pg.Pool.QueryRow(ctx,
		`SELECT count(*) FROM system_graph_versions WHERE graph_rid = $1`,
		"ri.vertex.main.graph.g2").Scan(&count); err != nil {
		t.Fatalf("count history failed: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 history rows for non-versioned graph, got %d", count)
	}
}

// TestVertexGraphsMigration_Given_GraphsExist_When_DownThenUp_Then_NoResidualRows
// validates that down + up wipes data with no FK violations.
func TestVertexGraphsMigration_Given_GraphsExist_When_DownThenUp_Then_NoResidualRows(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("first up failed: %v", err)
	}

	seedOntology(t, pg)

	if _, err := pg.Pool.Exec(ctx,
		`INSERT INTO system_graphs (rid, ontology_rid, name, version, versioned, payload, created_by)
		 VALUES ($1, $2, $3, 1, TRUE, $4::jsonb, $5)`,
		"ri.vertex.main.graph.g3", "ri.ontology.main.ontology.vtx", "Cycle Test",
		`{"layers":[],"edges":[]}`, "alice"); err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	if _, err := pg.Pool.Exec(ctx,
		`UPDATE system_graphs SET payload = $1::jsonb, version = 2 WHERE rid = $2`,
		`{"layers":[{"id":"L1"}],"edges":[]}`, "ri.vertex.main.graph.g3"); err != nil {
		t.Fatalf("update failed: %v", err)
	}

	if err := database.RunMigrationsDown(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration down failed: %v", err)
	}
	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("second up failed: %v", err)
	}

	for _, table := range []string{"system_graphs", "system_graph_versions"} {
		var count int
		if err := pg.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatalf("count on %s failed: %v", table, err)
		}
		if count != 0 {
			t.Errorf("expected %s to be empty after down+up, got %d rows", table, count)
		}
	}
}

func seedOntology(t *testing.T, pg *testutil.PGContainer) {
	t.Helper()
	if _, err := pg.Pool.Exec(context.Background(),
		`INSERT INTO ontologies (rid, api_name, display_name)
		 VALUES ($1, $2, $3) ON CONFLICT (rid) DO NOTHING`,
		"ri.ontology.main.ontology.vtx", "vtx", "Vertex"); err != nil {
		t.Fatalf("insert ontology failed: %v", err)
	}
}
