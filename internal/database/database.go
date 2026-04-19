package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig holds connection pool tuning parameters.
type PoolConfig struct {
	MaxConns          int32
	MaxConnLifetime   time.Duration
	HealthCheckPeriod time.Duration
}

// DefaultPoolConfig returns production-ready pool defaults.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConns:          25,
		MaxConnLifetime:   1 * time.Hour,
		HealthCheckPeriod: 30 * time.Second,
	}
}

// ParsePoolConfig parses a DSN and applies the given pool tuning parameters,
// returning a pgxpool.Config ready for use with pgxpool.NewWithConfig.
func ParsePoolConfig(dsn string, pc PoolConfig) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DSN: %w", err)
	}
	cfg.MaxConns = pc.MaxConns
	cfg.MaxConnLifetime = pc.MaxConnLifetime
	cfg.HealthCheckPeriod = pc.HealthCheckPeriod
	return cfg, nil
}

// Connect creates a connection pool using default settings and verifies
// connectivity with a ping.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return ConnectWithConfig(ctx, dsn, DefaultPoolConfig())
}

// ConnectWithConfig creates a connection pool with explicit pool tuning
// parameters and verifies connectivity with a ping.
func ConnectWithConfig(ctx context.Context, dsn string, pc PoolConfig) (*pgxpool.Pool, error) {
	return ConnectWithTracer(ctx, dsn, pc, nil)
}

// ConnectWithTracer is ConnectWithConfig plus an optional pgx.QueryTracer
// hook installed on the connection config. Wired by cmd/server when
// US-271 OpenTelemetry tracing is enabled so every PG query opens a
// span at pkg/tracing.PgxTracer. Pass tracer=nil to opt out — degraded
// mode and unit tests should leave it unset.
func ConnectWithTracer(ctx context.Context, dsn string, pc PoolConfig, tracer pgx.QueryTracer) (*pgxpool.Pool, error) {
	cfg, err := ParsePoolConfig(dsn, pc)
	if err != nil {
		return nil, err
	}
	if tracer != nil {
		cfg.ConnConfig.Tracer = tracer
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	return pool, nil
}
