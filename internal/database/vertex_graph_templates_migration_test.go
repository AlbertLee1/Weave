//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
)

// TestVertexGraphTemplatesMigration_Given_FreshDB_When_MigrateUp_Then_TableExists
// covers VTX-009: graph_templates migration shape.
func TestVertexGraphTemplatesMigration_Given_FreshDB_When_MigrateUp_Then_TableExists(t *testing.T) {
	pg := testutil.StartPGContainer(t)

	if err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir()); err != nil {
		t.Fatalf("migration up failed: %v", err)
	}
	if !pg.TableExists(t, "graph_templates") {
		t.Error("expected table graph_templates to exist")
	}

	ctx := context.Background()
	cols := []string{"rid", "source_graph_rid", "name", "payload",
		"parameterized_fields", "parameters", "created_by", "created_at"}
	for _, col := range cols {
		var exists bool
		err := pg.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'public' AND table_name = 'graph_templates' AND column_name = $1
			)`, col).Scan(&exists)
		if err != nil {
			t.Fatalf("column query for %s: %v", col, err)
		}
		if !exists {
			t.Errorf("expected column graph_templates.%s", col)
		}
	}
}
