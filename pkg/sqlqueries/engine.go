// Package sqlqueries implements the Foundry OSv2 SqlQueries.execute
// endpoint (POST /api/v2/sqlQueries/execute).
//
// The endpoint accepts a single read-only SELECT statement and returns a
// QueryStatus union (succeeded | failed). Weave runs all queries
// synchronously on the calling goroutine, so the "running" and "canceled"
// variants of the Foundry union are intentionally never produced — the
// types are documented for SDK compatibility but the wire path always
// resolves to a terminal state before responding.
package sqlqueries

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotSelect is returned when a non-SELECT statement is submitted.
// The handler maps this to the Foundry "failed" QueryStatus with the
// "NonSelectQuery" failureReason. Retained for backward compatibility
// with US-220 callers; new code should branch on the more granular
// ValidateQuery sentinels (ErrEmptyQuery, ErrStackedStatement,
// ErrForbiddenStatement, ErrSystemTableAccess) declared in safety.go.
var ErrNotSelect = errors.New("only SELECT statements are allowed")

// ErrQueryTimeout is returned when the per-query Config.Timeout elapses
// before the statement completes. The handler maps it to the Foundry
// "failed" QueryStatus with the "QueryTimeout" failureReason.
var ErrQueryTimeout = errors.New("query exceeded the configured timeout")

// ErrMaxRowsExceeded is returned when the result stream produces more
// rows than Config.MaxRows. The handler maps it to the Foundry "failed"
// QueryStatus with the "MaxRowsExceeded" failureReason.
var ErrMaxRowsExceeded = errors.New("query produced more rows than the configured cap")

// DefaultQueryTimeout is the upper bound on a single SQL query execution
// when Config.Timeout is unset. Matches the US-468 PRD contract (5s).
const DefaultQueryTimeout = 5 * time.Second

// DefaultMaxRows is the upper bound on the number of rows the engine
// will stream before aborting with ErrMaxRowsExceeded. Matches the
// US-468 PRD contract (10K rows).
const DefaultMaxRows = 10000

// Config holds the per-engine sandbox quotas enforced by PGEngine.
// Zero or negative values fall back to the package defaults so callers
// can override just the knob they care about.
type Config struct {
	// Timeout is the maximum wall-clock duration of a single Execute
	// call. The engine wraps the caller's context with context.WithTimeout
	// before running the query.
	Timeout time.Duration
	// MaxRows is the maximum number of rows the engine streams before
	// aborting with ErrMaxRowsExceeded. The cap is enforced on the
	// result side after the planner accepts the statement.
	MaxRows int
}

// DefaultConfig returns the US-468 contract: 5s timeout, 10K row cap.
func DefaultConfig() Config {
	return Config{Timeout: DefaultQueryTimeout, MaxRows: DefaultMaxRows}
}

// resolve fills in defaults for any zero / negative field on cfg.
func (c Config) resolve() Config {
	out := c
	if out.Timeout <= 0 {
		out.Timeout = DefaultQueryTimeout
	}
	if out.MaxRows <= 0 {
		out.MaxRows = DefaultMaxRows
	}
	return out
}

// Engine executes a validated SQL query against the underlying store.
// Implementations must NOT mutate state — the handler enforces SELECT-only
// at the wire layer, but engine implementations should still treat the
// call as read-only and run inside a read-only transaction when the
// backend supports it.
type Engine interface {
	Execute(ctx context.Context, query string) error
}

// PGEngine is a pgxpool-backed Engine. Each Execute call runs the given
// query inside a read-only transaction so any accidental side-effect
// (e.g. SELECT pg_advisory_lock) is rolled back when the function
// returns. The engine discards the result rows; this matches the
// single-machine Foundry parity scope where the response carries only the
// QueryStatus envelope, not the row payload.
type PGEngine struct {
	pool *pgxpool.Pool
	cfg  Config
}

// NewPGEngine wraps a pgx pool as an Engine using DefaultConfig.
func NewPGEngine(pool *pgxpool.Pool) *PGEngine {
	return NewPGEngineWithConfig(pool, DefaultConfig())
}

// NewPGEngineWithConfig wraps pool with an explicit Config. Any zero /
// negative Config field is filled from DefaultConfig so callers can
// override one knob without losing the other.
func NewPGEngineWithConfig(pool *pgxpool.Pool, cfg Config) *PGEngine {
	return &PGEngine{pool: pool, cfg: cfg.resolve()}
}

// Config returns the resolved sandbox config the engine is enforcing.
func (e *PGEngine) Config() Config { return e.cfg }

// Execute runs the query inside a read-only transaction. The caller's
// context is wrapped with the configured timeout, and the result stream
// is aborted with ErrMaxRowsExceeded once Config.MaxRows is reached.
func (e *PGEngine) Execute(ctx context.Context, query string) error {
	if err := ValidateQuery(query); err != nil {
		return err
	}
	execCtx, cancel := context.WithTimeout(ctx, e.cfg.Timeout)
	defer cancel()
	tx, err := e.pool.BeginTx(execCtx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return mapContextError(execCtx, err)
	}
	defer tx.Rollback(execCtx)
	rows, err := tx.Query(execCtx, query)
	if err != nil {
		return mapContextError(execCtx, err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > e.cfg.MaxRows {
			return ErrMaxRowsExceeded
		}
	}
	if err := rows.Err(); err != nil {
		return mapContextError(execCtx, err)
	}
	return nil
}

// mapContextError rewrites a pgx error caused by the per-query timeout
// into ErrQueryTimeout so callers can branch with errors.Is without
// guessing at PG-specific error codes. Any other error is returned
// verbatim.
func mapContextError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrQueryTimeout
	}
	return err
}

// IsSelectQuery returns true when the query passes ValidateQuery — i.e.
// a single read-only SELECT/WITH/VALUES/TABLE statement that does NOT
// reference pg_* or information_schema.* system tables. Retained for
// callers that just need a boolean verdict; new code should use
// ValidateQuery directly to surface the specific failure sentinel.
func IsSelectQuery(query string) bool {
	return ValidateQuery(query) == nil
}
