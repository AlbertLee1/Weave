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
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotSelect is returned when a non-SELECT statement is submitted.
// The handler maps this to the Foundry "failed" QueryStatus with the
// "NonSelectQuery" failureReason.
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
	if !IsSelectQuery(query) {
		return ErrNotSelect
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

// IsSelectQuery returns true when the query is a single read-only
// SELECT (or WITH ... SELECT) statement. Anything else — INSERT, UPDATE,
// DELETE, DDL, multi-statement, comment-prefixed injections — returns
// false. Comparison is case-insensitive on the first non-whitespace
// keyword and rejects any embedded ';' that would allow stacking a
// second statement.
func IsSelectQuery(query string) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return false
	}
	// Reject stacked statements. A trailing ';' is allowed only when no
	// non-whitespace characters follow it.
	if idx := strings.Index(q, ";"); idx >= 0 {
		tail := strings.TrimSpace(q[idx+1:])
		if tail != "" {
			return false
		}
	}
	upper := strings.ToUpper(q)
	return hasKeywordPrefix(upper, "SELECT") || hasKeywordPrefix(upper, "WITH")
}

// hasKeywordPrefix returns true if s starts with kw and the next byte is
// either absent or whitespace / '(' — preventing false positives like
// "SELECTOR" being matched against "SELECT".
func hasKeywordPrefix(s, kw string) bool {
	if !strings.HasPrefix(s, kw) {
		return false
	}
	if len(s) == len(kw) {
		return true
	}
	switch s[len(kw)] {
	case ' ', '\t', '\n', '\r', '(':
		return true
	}
	return false
}
