package jdbc

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Dialect identifies a SQL dialect family the connector knows how to
// quote identifiers / build placeholders for. Connector behavior
// otherwise relies on the standard SQL subset that all three dialects
// implement (SELECT … FROM … WHERE … ORDER BY … LIMIT …).
type Dialect string

const (
	// DialectPostgres covers PostgreSQL via lib/pq, jackc/pgx (stdlib
	// adapter), or any other database/sql driver speaking PG syntax.
	DialectPostgres Dialect = "postgres"
	// DialectMySQL covers go-sql-driver/mysql.
	DialectMySQL Dialect = "mysql"
	// DialectSQLite covers modernc.org/sqlite, glebarez/go-sqlite, or any
	// driver producing SQLite-compatible SQL.
	DialectSQLite Dialect = "sqlite"
)

// driverNameAliases maps common database/sql driver names operators
// register at boot to the dialect family they belong to. Postgres has
// the most aliases because pgx, lib/pq, and the bare "postgres" name
// are all in the wild.
var driverNameAliases = map[string]Dialect{
	"postgres":   DialectPostgres,
	"postgresql": DialectPostgres,
	"pgx":        DialectPostgres,
	"pq":         DialectPostgres,
	"mysql":      DialectMySQL,
	"sqlite":     DialectSQLite,
	"sqlite3":    DialectSQLite,
}

// DialectFromDriver resolves a database/sql driver name to its dialect
// family. Unknown / empty names error so the caller surfaces the
// misconfiguration before opening a connection.
func DialectFromDriver(driver string) (Dialect, error) {
	if driver == "" {
		return "", errors.New("jdbc: driver name must not be empty")
	}
	d, ok := driverNameAliases[strings.ToLower(driver)]
	if !ok {
		return "", fmt.Errorf("jdbc: unsupported driver %q (supported: postgres/postgresql/pgx/pq, mysql, sqlite/sqlite3)", driver)
	}
	return d, nil
}

// Quote wraps ident in the dialect's identifier-quoting characters. A
// single dot in ident is treated as a schema/table separator and each
// part is quoted independently; multiple dots are not supported.
//
// ident MUST already have been validated via ValidateIdentifier — Quote
// trusts its input. Callers that hand untrusted strings to Quote will
// produce SQL injection vulnerabilities.
func (d Dialect) Quote(ident string) string {
	parts := strings.Split(ident, ".")
	switch d {
	case DialectMySQL:
		for i, p := range parts {
			parts[i] = "`" + p + "`"
		}
	default:
		// Postgres and SQLite both use double-quoted identifiers per the
		// SQL standard. Other dialects fall through here too — safer than
		// emitting unquoted identifiers.
		for i, p := range parts {
			parts[i] = `"` + p + `"`
		}
	}
	return strings.Join(parts, ".")
}

// Placeholder returns the dialect-appropriate parameter placeholder for
// argument index n (1-based). Postgres uses $1/$2/…; MySQL and SQLite
// use ? for every position.
func (d Dialect) Placeholder(n int) string {
	if d == DialectPostgres {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

// identifierRE pins the allowed characters in a single identifier
// segment. The rule mirrors PostgreSQL's "regular identifier" grammar
// minus case folding: ASCII letter or underscore followed by letters,
// digits, or underscores. Length cap mirrors the SQL standard's 128.
var identifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// ValidateIdentifier rejects any value that wouldn't survive a round
// trip through Quote without enabling SQL injection. Allows an optional
// single schema prefix ("schema.table"); more than one dot is rejected
// to avoid ambiguity in dialects that accept catalog.schema.table —
// callers that need three-part names should split into two configured
// values.
func ValidateIdentifier(ident string) error {
	if ident == "" {
		return errors.New("jdbc: identifier must not be empty")
	}
	parts := strings.Split(ident, ".")
	if len(parts) > 2 {
		return fmt.Errorf("jdbc: identifier %q has too many dot-separated parts (max 2)", ident)
	}
	for _, p := range parts {
		if !identifierRE.MatchString(p) {
			return fmt.Errorf("jdbc: identifier segment %q is invalid (allowed: ASCII letter/underscore start, length 1..128, [A-Za-z0-9_])", p)
		}
	}
	return nil
}

// buildSelectSQL renders the SELECT statement for one page read.
//
// Pagination semantics:
//   - Incremental (cfg.IncrementalColumn != ""): keyset pagination on
//     that column. cursor is the last watermark value seen; empty cursor
//     means "first page". Order is ASC on the incremental column.
//   - Non-incremental: OFFSET pagination. cursor is the textual offset
//     (decimal); empty cursor means offset=0. Order is whatever the
//     caller declared in cfg.OrderBy.
//
// All dialects share the same skeleton — Postgres/MySQL/SQLite agree on
// LIMIT/OFFSET syntax and basic SELECT shape.
func buildSelectSQL(d Dialect, cfg Config, cursor string, offset int) (string, []any, error) {
	if err := validateSelectIdentifiers(cfg); err != nil {
		return "", nil, err
	}

	var b strings.Builder
	writeSelectClause(&b, d, cfg.Columns)
	b.WriteString(" FROM ")
	b.WriteString(d.Quote(cfg.Table))

	args := []any{}
	if cfg.IncrementalColumn != "" && cursor != "" {
		fmt.Fprintf(&b, ` WHERE %s > %s`, d.Quote(cfg.IncrementalColumn), d.Placeholder(1))
		args = append(args, cursor)
	}

	writeOrderByClause(&b, d, cfg)

	fmt.Fprintf(&b, " LIMIT %d", cfg.effectivePageSize())
	if cfg.IncrementalColumn == "" {
		fmt.Fprintf(&b, " OFFSET %d", offset)
	}
	return b.String(), args, nil
}

// validateSelectIdentifiers checks every identifier that buildSelectSQL
// would otherwise interpolate into the query string. Pulled out so the
// outer function stays under the gocyclo:15 lint floor.
func validateSelectIdentifiers(cfg Config) error {
	if err := ValidateIdentifier(cfg.Table); err != nil {
		return fmt.Errorf("table: %w", err)
	}
	for i, c := range cfg.Columns {
		if err := ValidateIdentifier(c); err != nil {
			return fmt.Errorf("columns[%d]: %w", i, err)
		}
	}
	for i, c := range cfg.OrderBy {
		if err := ValidateIdentifier(c); err != nil {
			return fmt.Errorf("orderBy[%d]: %w", i, err)
		}
	}
	if cfg.IncrementalColumn != "" {
		if err := ValidateIdentifier(cfg.IncrementalColumn); err != nil {
			return fmt.Errorf("incrementalColumn: %w", err)
		}
	}
	return nil
}

// writeSelectClause emits "SELECT *" when no projection is set,
// otherwise emits the comma-separated quoted column list.
func writeSelectClause(b *strings.Builder, d Dialect, columns []string) {
	b.WriteString("SELECT ")
	if len(columns) == 0 {
		b.WriteString("*")
		return
	}
	quoted := make([]string, len(columns))
	for i, c := range columns {
		quoted[i] = d.Quote(c)
	}
	b.WriteString(strings.Join(quoted, ", "))
}

// writeOrderByClause appends ORDER BY using the watermark column (when
// incremental) or the explicit OrderBy list (otherwise).
func writeOrderByClause(b *strings.Builder, d Dialect, cfg Config) {
	orderCols := cfg.OrderBy
	if cfg.IncrementalColumn != "" {
		orderCols = []string{cfg.IncrementalColumn}
	}
	if len(orderCols) == 0 {
		return
	}
	quoted := make([]string, len(orderCols))
	for i, c := range orderCols {
		quoted[i] = d.Quote(c) + " ASC"
	}
	b.WriteString(" ORDER BY ")
	b.WriteString(strings.Join(quoted, ", "))
}
