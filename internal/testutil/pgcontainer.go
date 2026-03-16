package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type PGContainer struct {
	Container *postgres.PostgresContainer
	DSN       string
	Pool      *pgxpool.Pool
}

func StartPGContainer(t *testing.T) *PGContainer {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("weave_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start postgres container: %v", err)
	}

	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate postgres container: %v", err)
		}
	})

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("failed to get connection string: %v", err)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to create pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	// Wait for pool to be ready
	for i := range 10 {
		if err := pool.Ping(ctx); err == nil {
			break
		}
		if i == 9 {
			t.Fatal("postgres pool not ready after 10 attempts")
		}
		time.Sleep(500 * time.Millisecond)
	}

	return &PGContainer{
		Container: pgContainer,
		DSN:       dsn,
		Pool:      pool,
	}
}

func MigrationsDir() string {
	return findMigrationsDir()
}

func findMigrationsDir() string {
	// Walk up from test location to find migrations/
	paths := []string{
		"../../migrations",
		"../../../migrations",
		"../../../../migrations",
	}
	for _, p := range paths {
		return p
	}
	return "migrations"
}

func (pg *PGContainer) TableExists(t *testing.T, tableName string) bool {
	t.Helper()
	var exists bool
	err := pg.Pool.QueryRow(context.Background(),
		"SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)",
		tableName,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("failed to check table existence: %v", err)
	}
	return exists
}

func (pg *PGContainer) AllTables(t *testing.T) []string {
	t.Helper()
	rows, err := pg.Pool.Query(context.Background(),
		"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name")
	if err != nil {
		t.Fatalf("failed to query tables: %v", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("failed to scan table name: %v", err)
		}
		tables = append(tables, name)
	}
	return tables
}

func (pg *PGContainer) DSNForMigrate() string {
	return fmt.Sprintf("%s", pg.DSN)
}
