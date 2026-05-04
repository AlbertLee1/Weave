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
}

// NewPGEngine wraps a pgx pool as an Engine.
func NewPGEngine(pool *pgxpool.Pool) *PGEngine {
	return &PGEngine{pool: pool}
}

// Execute runs the query inside a read-only transaction.
func (e *PGEngine) Execute(ctx context.Context, query string) error {
	if err := ValidateQuery(query); err != nil {
		return err
	}
	tx, err := e.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		// drain
	}
	return rows.Err()
}

// IsSelectQuery returns true when the query passes ValidateQuery — i.e.
// a single read-only SELECT/WITH/VALUES/TABLE statement that does NOT
// reference pg_* or information_schema.* system tables. Retained for
// callers that just need a boolean verdict; new code should use
// ValidateQuery directly to surface the specific failure sentinel.
func IsSelectQuery(query string) bool {
	return ValidateQuery(query) == nil
}
