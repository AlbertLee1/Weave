// Package jdbc implements a JDBC-style read connector backed by Go's
// database/sql abstraction (US-292). The connector is dialect-aware
// across PostgreSQL, MySQL, and SQLite — operators register the desired
// driver (lib/pq, jackc/pgx/v5/stdlib, go-sql-driver/mysql,
// modernc.org/sqlite, …) and pass the driver name + DSN through Config.
//
// Pagination + incremental field support:
//
//   - Set Config.IncrementalColumn to enable keyset pagination on a
//     monotonically-increasing watermark column. Each ReadPage call
//     returns the page rows plus the next watermark cursor; subsequent
//     pages narrow to "WHERE col > cursor".
//   - Leave IncrementalColumn empty for full-table reads. The connector
//     falls back to LIMIT/OFFSET pagination, requiring an explicit
//     OrderBy so the page order is stable across calls.
//
// The package itself imports no SQL driver — keeping it driver-agnostic
// preserves the project's CGO_ENABLED=0 invariant (any pure-Go driver
// works) and lets each operator pick the driver that fits their build.
//
// Example wiring (in cmd/server or a connector loader):
//
//	import _ "github.com/jackc/pgx/v5/stdlib"  // registers "pgx"
//	c, err := jdbc.New(jdbc.Config{
//	    Driver:            "pgx",
//	    DSN:               "postgres://…",
//	    Table:             "events",
//	    PageSize:          500,
//	    IncrementalColumn: "updated_at",
//	})
//	for cursor, more := "", true; more; {
//	    rows, next, hasMore, err := c.ReadPage(ctx, cursor)
//	    …
//	    cursor, more = next, hasMore
//	}
package jdbc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
)

// DefaultPageSize is the page size used when Config.PageSize <= 0.
// Mirrors the conservative default of 1000 used by the schema-inference
// sample budget; large enough to amortize round-trips, small enough to
// keep per-batch memory bounded for wide tables.
const DefaultPageSize = 1000

// MaxPageSize caps the per-page row count so a misconfigured caller
// can't pull a 10M-row table into memory in one shot. Callers that need
// more should iterate over pages.
const MaxPageSize = 100000

// Config describes one JDBC read source. Validate is the source of
// truth for what counts as a well-formed config; New / NewWithDB call
// it before doing any work.
type Config struct {
	// Driver is the database/sql driver name as registered via
	// sql.Register (e.g. "pgx", "postgres", "mysql", "sqlite"). Required
	// for New(); ignored by NewWithDB() which takes a pre-opened *sql.DB.
	Driver string

	// DSN is the connector-specific connection string. Required for
	// New(); ignored by NewWithDB().
	DSN string

	// Table is the source table the connector reads from. May be a bare
	// table name ("users") or a single dot-separated schema-qualified
	// name ("public.users"). Both segments are validated against the
	// regular-identifier grammar.
	Table string

	// Columns is the optional projection. Empty (the default) emits
	// "SELECT *" so every column round-trips.
	Columns []string

	// PageSize bounds the rows returned per ReadPage call. Defaults to
	// DefaultPageSize; values above MaxPageSize are clamped on read.
	PageSize int

	// IncrementalColumn enables keyset pagination on a watermark column.
	// When non-empty, the connector orders by this column ASC and tracks
	// the last value seen as the cursor for subsequent pages — appends
	// to the source table after the read started will surface in later
	// pages without producing duplicates.
	IncrementalColumn string

	// OrderBy is the explicit column list for non-incremental reads.
	// Required when IncrementalColumn is empty (otherwise OFFSET-based
	// pagination is non-deterministic across calls). Ignored when
	// IncrementalColumn is set — the watermark column dictates order.
	OrderBy []string
}

// effectivePageSize applies the default + cap rules.
func (c *Config) effectivePageSize() int {
	if c.PageSize <= 0 {
		return DefaultPageSize
	}
	if c.PageSize > MaxPageSize {
		return MaxPageSize
	}
	return c.PageSize
}

// Validate reports the first structural issue with c. Pure function;
// safe to call from admin handlers / pipeline-DSL parsers before
// attempting to open a connection.
func (c Config) Validate() error {
	if c.Table == "" {
		return errors.New("jdbc: Config.Table must not be empty")
	}
	if err := ValidateIdentifier(c.Table); err != nil {
		return err
	}
	if c.PageSize < 0 {
		return fmt.Errorf("jdbc: Config.PageSize must be >= 0 (got %d)", c.PageSize)
	}
	for i, col := range c.Columns {
		if err := ValidateIdentifier(col); err != nil {
			return fmt.Errorf("jdbc: Config.Columns[%d]: %w", i, err)
		}
	}
	for i, col := range c.OrderBy {
		if err := ValidateIdentifier(col); err != nil {
			return fmt.Errorf("jdbc: Config.OrderBy[%d]: %w", i, err)
		}
	}
	if c.IncrementalColumn != "" {
		if err := ValidateIdentifier(c.IncrementalColumn); err != nil {
			return fmt.Errorf("jdbc: Config.IncrementalColumn: %w", err)
		}
	} else if len(c.OrderBy) == 0 {
		// OFFSET pagination without an ORDER BY produces non-deterministic
		// pages; reject at config time so misconfigured callers don't
		// silently double-read or skip rows.
		return errors.New("jdbc: Config.OrderBy must declare at least one column when IncrementalColumn is empty")
	}
	return nil
}

// Connector is one open JDBC source. Connectors are safe for concurrent
// use; *sql.DB itself pools connections.
type Connector struct {
	db      *sql.DB
	cfg     Config
	dialect Dialect
}

// New opens a *sql.DB via sql.Open(cfg.Driver, cfg.DSN) and wraps it in
// a Connector. The driver MUST already be registered by the caller via
// a side-effect import (e.g. `import _ "github.com/jackc/pgx/v5/stdlib"`).
func New(cfg Config) (*Connector, error) {
	if cfg.Driver == "" {
		return nil, errors.New("jdbc: Config.Driver must not be empty for New(); use NewWithDB to inject a *sql.DB")
	}
	dialect, err := DialectFromDriver(cfg.Driver)
	if err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	db, err := sql.Open(cfg.Driver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("jdbc: sql.Open(%q): %w", cfg.Driver, err)
	}
	return &Connector{db: db, cfg: cfg, dialect: dialect}, nil
}

// NewWithDB wraps a pre-opened *sql.DB in a Connector. Useful for tests
// (inject a fake driver) and for callers that share a *sql.DB across
// multiple connectors / non-connector queries.
func NewWithDB(db *sql.DB, cfg Config) (*Connector, error) {
	if db == nil {
		return nil, errors.New("jdbc: NewWithDB requires a non-nil *sql.DB")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	dialect := DialectPostgres
	if cfg.Driver != "" {
		var err error
		dialect, err = DialectFromDriver(cfg.Driver)
		if err != nil {
			return nil, err
		}
	}
	return &Connector{db: db, cfg: cfg, dialect: dialect}, nil
}

// Close releases the underlying *sql.DB. Calling ReadPage after Close
// returns the database/sql "sql: database is closed" error.
func (c *Connector) Close() error {
	if c == nil || c.db == nil {
		return nil
	}
	return c.db.Close()
}

// ReadPage runs one paginated SELECT against the source table.
//
// cursor semantics:
//   - "" — first page.
//   - non-incremental config — decimal row offset.
//   - incremental config — last value of the IncrementalColumn observed
//     by the previous ReadPage call.
//
// Return values:
//   - rows: the decoded page; each map keys columns by their declared
//     name (or alias). Values are whatever database/sql produces for the
//     driver's SQL type — typically int64 for integers, []byte for text
//     in some drivers, time.Time for timestamps, etc.
//   - nextCursor: the cursor to pass on the next call. Empty when
//     hasMore is false.
//   - hasMore: true when the page filled exactly (page rows == PageSize)
//     so the caller should fetch again. False when a partial page (or
//     zero rows) signals end-of-stream.
//   - err: surfaced from sql.QueryContext, sql.Rows.Scan, etc. The page
//     is returned untouched on err so callers may resume from the same
//     cursor on the next attempt.
//
// hasMore can produce one redundant call when the source has exactly
// PageSize rows after the cursor — the next call returns 0 rows + more
// = false. That's the standard tradeoff for cursor pagination without a
// COUNT(*) round-trip.
func (c *Connector) ReadPage(ctx context.Context, cursor string) (rows []map[string]any, nextCursor string, hasMore bool, err error) {
	offset := 0
	if c.cfg.IncrementalColumn == "" && cursor != "" {
		n, parseErr := strconv.Atoi(cursor)
		if parseErr != nil || n < 0 {
			return nil, "", false, fmt.Errorf("jdbc: cursor %q is not a non-negative integer offset", cursor)
		}
		offset = n
	}
	stmt, args, err := buildSelectSQL(c.dialect, c.cfg, cursor, offset)
	if err != nil {
		return nil, "", false, err
	}
	dbRows, err := c.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, "", false, fmt.Errorf("jdbc: query: %w", err)
	}
	defer dbRows.Close()

	cols, err := dbRows.Columns()
	if err != nil {
		return nil, "", false, fmt.Errorf("jdbc: columns: %w", err)
	}
	rows = make([]map[string]any, 0)
	page := c.cfg.effectivePageSize()
	for dbRows.Next() {
		dest := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range dest {
			ptrs[i] = &dest[i]
		}
		if err := dbRows.Scan(ptrs...); err != nil {
			return nil, "", false, fmt.Errorf("jdbc: scan: %w", err)
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = dest[i]
		}
		rows = append(rows, row)
	}
	if err := dbRows.Err(); err != nil {
		return nil, "", false, fmt.Errorf("jdbc: rows: %w", err)
	}

	hasMore = len(rows) == page
	if !hasMore {
		return rows, "", false, nil
	}
	if c.cfg.IncrementalColumn != "" {
		// Watermark cursor: stringify the last row's incremental column.
		last := rows[len(rows)-1][c.cfg.IncrementalColumn]
		nextCursor = stringifyWatermark(last)
	} else {
		nextCursor = strconv.Itoa(offset + len(rows))
	}
	return rows, nextCursor, true, nil
}

// stringifyWatermark renders a watermark value as the cursor string that
// later pages echo back. Strings round-trip verbatim; everything else
// goes through fmt.Sprintf("%v") which handles int64 / time.Time /
// []byte / etc. The driver's WHERE-comparison must accept the returned
// string under SQL parameter binding — Postgres / MySQL / SQLite all
// coerce TEXT params to the column type, so this is safe in practice.
func stringifyWatermark(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return fmt.Sprintf("%v", v)
}
