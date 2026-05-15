package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StartTimescaleDBContainer boots a TimescaleDB container (Postgres 16 + the
// timescaledb extension preloaded) for VTX-028+ time series tests. We use the
// regular `timescale/timescaledb:latest-pg16` image (~1.4 GB) rather than the
// HA superset (~3 GB) because VTX-028 only needs core hypertable + continuous
// aggregate features — pgvector / postgis are not required on the timeseries
// codepath. The helper stays separate from StartPGContainer so the rest of
// the integration suite — which depends on pgvector for embeddings — keeps
// running against the lighter pgvector/pgvector:pg16 image.
func StartTimescaleDBContainer(t testing.TB) *PGContainer {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"timescale/timescaledb:latest-pg16",
		postgres.WithDatabase("weave_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("failed to start timescaledb container: %v", err)
	}

	t.Cleanup(func() {
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("failed to terminate timescaledb container: %v", err)
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

	for i := range 10 {
		if err := pool.Ping(ctx); err == nil {
			break
		}
		if i == 9 {
			t.Fatal("timescaledb pool not ready after 10 attempts")
		}
		time.Sleep(500 * time.Millisecond)
	}

	return &PGContainer{
		Container: pgContainer,
		DSN:       dsn,
		Pool:      pool,
	}
}
