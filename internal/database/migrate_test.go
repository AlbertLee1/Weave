//go:build integration

package database_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/internal/database"
	"github.com/liyang/weave/internal/testutil"
)

func TestMigrateUp_CreatesAllTables(t *testing.T) {
	pg := testutil.StartPGContainer(t)

	err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	expectedTables := []string{
		"ontologies",
		"object_types",
		"properties",
		"link_types",
		"action_types",
		"interfaces",
		"object_type_interfaces",
		"value_types",
		"datasource_bindings",
		"security_policies",
		"action_logs",
	}

	for _, table := range expectedTables {
		if !pg.TableExists(t, table) {
			t.Errorf("expected table %q to exist", table)
		}
	}
}

func TestMigrateUp_Idempotent(t *testing.T) {
	pg := testutil.StartPGContainer(t)

	err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("first migration failed: %v", err)
	}

	err = database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("second migration should be idempotent: %v", err)
	}
}

func TestMigrateDown_DropsAllTables(t *testing.T) {
	pg := testutil.StartPGContainer(t)

	err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("migration up failed: %v", err)
	}

	err = database.RunMigrationsDown(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("migration down failed: %v", err)
	}

	tables := pg.AllTables(t)
	// Only schema_migrations should remain (or empty)
	for _, table := range tables {
		if table != "schema_migrations" {
			t.Errorf("table %q should have been dropped", table)
		}
	}
}

func TestSchema_OntologiesTable(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Insert
	_, err = pg.Pool.Exec(ctx,
		"INSERT INTO ontologies (rid, api_name, display_name, description) VALUES ($1, $2, $3, $4)",
		"ri.ontology.main.ontology.test-1", "test-ontology", "Test Ontology", "A test")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Read back
	var apiName, displayName string
	err = pg.Pool.QueryRow(ctx,
		"SELECT api_name, display_name FROM ontologies WHERE rid = $1",
		"ri.ontology.main.ontology.test-1").Scan(&apiName, &displayName)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if apiName != "test-ontology" {
		t.Errorf("expected api_name 'test-ontology', got %q", apiName)
	}
	if displayName != "Test Ontology" {
		t.Errorf("expected display_name 'Test Ontology', got %q", displayName)
	}
}

func TestSchema_ObjectTypesTable_ForeignKey(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Try to insert object_type without ontology — should fail FK
	_, err = pg.Pool.Exec(ctx,
		`INSERT INTO object_types (rid, ontology_rid, api_name, display_name, primary_key_prop)
		 VALUES ($1, $2, $3, $4, $5)`,
		"ri.ontology.main.object-type.t1", "ri.ontology.main.ontology.nonexistent",
		"employee", "Employee", "employeeId")
	if err == nil {
		t.Fatal("expected FK violation error, got nil")
	}
}

func TestSchema_PropertiesTable_CascadeDelete(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Setup: ontology → object_type → property
	_, err = pg.Pool.Exec(ctx,
		"INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)",
		"ri.ontology.main.ontology.o1", "test", "Test")
	if err != nil {
		t.Fatalf("insert ontology failed: %v", err)
	}

	_, err = pg.Pool.Exec(ctx,
		`INSERT INTO object_types (rid, ontology_rid, api_name, display_name, primary_key_prop)
		 VALUES ($1, $2, $3, $4, $5)`,
		"ri.ontology.main.object-type.ot1", "ri.ontology.main.ontology.o1",
		"employee", "Employee", "id")
	if err != nil {
		t.Fatalf("insert object_type failed: %v", err)
	}

	_, err = pg.Pool.Exec(ctx,
		`INSERT INTO properties (rid, object_type_rid, api_name, base_type) VALUES ($1, $2, $3, $4)`,
		"ri.ontology.main.property.p1", "ri.ontology.main.object-type.ot1", "name", "string")
	if err != nil {
		t.Fatalf("insert property failed: %v", err)
	}

	// Delete object_type — property should cascade
	_, err = pg.Pool.Exec(ctx,
		"DELETE FROM object_types WHERE rid = $1", "ri.ontology.main.object-type.ot1")
	if err != nil {
		t.Fatalf("delete object_type failed: %v", err)
	}

	var count int
	pg.Pool.QueryRow(ctx, "SELECT count(*) FROM properties WHERE rid = $1",
		"ri.ontology.main.property.p1").Scan(&count)
	if count != 0 {
		t.Error("expected property to be cascade-deleted")
	}
}

func TestSchema_UniqueConstraints(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Insert first ontology
	_, err = pg.Pool.Exec(ctx,
		"INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)",
		"ri.ontology.main.ontology.u1", "unique-name", "First")
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Insert duplicate api_name — should fail
	_, err = pg.Pool.Exec(ctx,
		"INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)",
		"ri.ontology.main.ontology.u2", "unique-name", "Second")
	if err == nil {
		t.Fatal("expected unique constraint violation, got nil")
	}
}

func TestSchema_CheckConstraints(t *testing.T) {
	pg := testutil.StartPGContainer(t)
	ctx := context.Background()

	err := database.RunMigrationsUp(pg.DSN, testutil.MigrationsDir())
	if err != nil {
		t.Fatalf("migration failed: %v", err)
	}

	// Setup ontology
	_, err = pg.Pool.Exec(ctx,
		"INSERT INTO ontologies (rid, api_name, display_name) VALUES ($1, $2, $3)",
		"ri.ontology.main.ontology.c1", "check-test", "Check Test")
	if err != nil {
		t.Fatalf("insert ontology failed: %v", err)
	}

	// Insert object_type with invalid status
	_, err = pg.Pool.Exec(ctx,
		`INSERT INTO object_types (rid, ontology_rid, api_name, display_name, primary_key_prop, status)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		"ri.ontology.main.object-type.c1", "ri.ontology.main.ontology.c1",
		"test", "Test", "id", "INVALID_STATUS")
	if err == nil {
		t.Fatal("expected check constraint violation for invalid status, got nil")
	}
}
