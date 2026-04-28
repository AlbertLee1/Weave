package jdbc

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

// fakeDriver is a minimal database/sql driver used by every test in this
// package. Tests register a driver instance with `sql.Register`, set up
// canned rows via `setRows(query → rows)`, and assert on the queries the
// connector sent via `recorded()`. Pure Go, no external dep.
//
// fakeDriver is shared across the package's test files via a global
// registration in TestMain — registering twice in one process panics, so
// we register a single, mutable instance that test cases reset between
// runs.
type fakeDriver struct {
	mu       sync.Mutex
	queries  []recordedQuery
	rules    []responseRule
	openErr  error
	queryErr error
}

type recordedQuery struct {
	query string
	args  []driver.NamedValue
}

type responseRule struct {
	match string // substring match against the query string
	cols  []string
	rows  [][]driver.Value
}

func (d *fakeDriver) Open(name string) (driver.Conn, error) {
	if d.openErr != nil {
		return nil, d.openErr
	}
	return &fakeConn{drv: d}, nil
}

func (d *fakeDriver) reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.queries = nil
	d.rules = nil
	d.openErr = nil
	d.queryErr = nil
}

func (d *fakeDriver) addRule(match string, cols []string, rows [][]driver.Value) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rules = append(d.rules, responseRule{match: match, cols: cols, rows: rows})
}

func (d *fakeDriver) recorded() []recordedQuery {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]recordedQuery, len(d.queries))
	copy(out, d.queries)
	return out
}

func (d *fakeDriver) lookup(query string) (cols []string, rows [][]driver.Value, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.rules {
		if strings.Contains(query, r.match) {
			return r.cols, r.rows, true
		}
	}
	return nil, nil, false
}

type fakeConn struct{ drv *fakeDriver }

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) {
	return &fakeStmt{drv: c.drv, query: query}, nil
}
func (c *fakeConn) Close() error              { return nil }
func (c *fakeConn) Begin() (driver.Tx, error) { return nil, fmt.Errorf("not implemented") }

// QueryerContext lets us see the named args + ctx pass through.
func (c *fakeConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.drv.mu.Lock()
	c.drv.queries = append(c.drv.queries, recordedQuery{query: query, args: args})
	if c.drv.queryErr != nil {
		err := c.drv.queryErr
		c.drv.mu.Unlock()
		return nil, err
	}
	c.drv.mu.Unlock()
	cols, rows, _ := c.drv.lookup(query)
	return &fakeRows{cols: cols, data: rows}, nil
}

type fakeStmt struct {
	drv   *fakeDriver
	query string
}

func (s *fakeStmt) Close() error  { return nil }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec(args []driver.Value) (driver.Result, error) {
	return nil, fmt.Errorf("exec unsupported")
}

func (s *fakeStmt) Query(args []driver.Value) (driver.Rows, error) {
	named := make([]driver.NamedValue, len(args))
	for i, a := range args {
		named[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	s.drv.mu.Lock()
	s.drv.queries = append(s.drv.queries, recordedQuery{query: s.query, args: named})
	if s.drv.queryErr != nil {
		err := s.drv.queryErr
		s.drv.mu.Unlock()
		return nil, err
	}
	s.drv.mu.Unlock()
	cols, rows, _ := s.drv.lookup(s.query)
	return &fakeRows{cols: cols, data: rows}, nil
}

type fakeRows struct {
	cols []string
	data [][]driver.Value
	idx  int
}

func (r *fakeRows) Columns() []string { return r.cols }
func (r *fakeRows) Close() error      { return nil }
func (r *fakeRows) Next(dest []driver.Value) error {
	if r.idx >= len(r.data) {
		return io.EOF
	}
	row := r.data[r.idx]
	r.idx++
	for i, v := range row {
		if i >= len(dest) {
			break
		}
		dest[i] = v
	}
	return nil
}

// fakeDriverInstance is the package-shared driver. TestMain registers it
// under "weave-test" exactly once.
var fakeDriverInstance = &fakeDriver{}

func TestMain(m *testing.M) {
	// Register the same fake-driver instance under a generic test name
	// (used by NewWithDB callers that don't care about dialect) AND
	// under a dialect-recognized alias so TestNew_OpenDriver can exercise
	// the New() → sql.Open path without pulling in a real driver.
	sql.Register("weave-test", fakeDriverInstance)
	sql.Register("weave-test-pg", fakeDriverInstance)
	driverNameAliases["weave-test-pg"] = DialectPostgres
	m.Run()
}

// openTestDB resets the fake driver and opens a fresh *sql.DB.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	fakeDriverInstance.reset()
	db, err := sql.Open("weave-test", "test://memory")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestNewWithDB_RequiresValidConfig(t *testing.T) {
	db := openTestDB(t)
	if _, err := NewWithDB(db, Config{}); err == nil {
		t.Fatal("empty config should be rejected")
	}
	if _, err := NewWithDB(nil, Config{Table: "users", PageSize: 10}); err == nil {
		t.Fatal("nil db should be rejected")
	}
}

func TestConnector_ReadPage_NonIncremental(t *testing.T) {
	db := openTestDB(t)
	fakeDriverInstance.addRule(
		`FROM "users"`,
		[]string{"id", "name"},
		[][]driver.Value{
			{int64(1), "alice"},
			{int64(2), "bob"},
		},
	)
	c, err := NewWithDB(db, Config{
		Driver:   "postgres",
		Table:    "users",
		Columns:  []string{"id", "name"},
		PageSize: 2,
		OrderBy:  []string{"id"},
	})
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}
	rows, next, more, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0]["id"] != int64(1) || rows[0]["name"] != "alice" {
		t.Errorf("row[0]=%v", rows[0])
	}
	// page filled (PageSize=2, returned 2) ⇒ more=true and next is offset.
	if !more {
		t.Errorf("more=false; want true (full page returned)")
	}
	if next != "2" {
		t.Errorf("next cursor = %q want %q", next, "2")
	}

	q := fakeDriverInstance.recorded()
	if len(q) != 1 {
		t.Fatalf("want 1 query, got %d", len(q))
	}
	if !strings.Contains(q[0].query, "OFFSET 0") {
		t.Errorf("first page should OFFSET 0: %q", q[0].query)
	}
}

func TestConnector_ReadPage_NonIncremental_OffsetAdvances(t *testing.T) {
	db := openTestDB(t)
	fakeDriverInstance.addRule(
		`FROM "users"`,
		[]string{"id"},
		[][]driver.Value{{int64(3)}},
	)
	c, err := NewWithDB(db, Config{
		Driver:   "postgres",
		Table:    "users",
		PageSize: 5,
		OrderBy:  []string{"id"},
	})
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}
	rows, next, more, err := c.ReadPage(context.Background(), "10")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows=%d want 1", len(rows))
	}
	if more {
		t.Errorf("more=true; partial page should be more=false")
	}
	if next != "" {
		t.Errorf("next=%q want empty (no more)", next)
	}
	q := fakeDriverInstance.recorded()
	if !strings.Contains(q[0].query, "OFFSET 10") {
		t.Errorf("query must carry OFFSET 10: %q", q[0].query)
	}
}

func TestConnector_ReadPage_Incremental(t *testing.T) {
	db := openTestDB(t)
	fakeDriverInstance.addRule(
		`FROM "events"`,
		[]string{"id", "ts"},
		[][]driver.Value{
			{int64(1), "2024-01-01"},
			{int64(2), "2024-01-02"},
		},
	)
	c, err := NewWithDB(db, Config{
		Driver:            "postgres",
		Table:             "events",
		PageSize:          2,
		IncrementalColumn: "ts",
	})
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}
	rows, next, more, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d want 2", len(rows))
	}
	if !more {
		t.Errorf("full page returned ⇒ more=true")
	}
	if next != "2024-01-02" {
		t.Errorf("next=%q want last watermark 2024-01-02", next)
	}

	// Second page uses the watermark.
	rows2, next2, more2, err := c.ReadPage(context.Background(), next)
	if err != nil {
		t.Fatalf("ReadPage 2: %v", err)
	}
	_ = rows2
	_ = more2
	q := fakeDriverInstance.recorded()
	if len(q) != 2 {
		t.Fatalf("want 2 queries, got %d", len(q))
	}
	if !strings.Contains(q[1].query, `WHERE "ts" > $1`) {
		t.Errorf("watermark page missing WHERE: %q", q[1].query)
	}
	if len(q[1].args) != 1 || q[1].args[0].Value != "2024-01-02" {
		t.Errorf("watermark args=%v want [2024-01-02]", q[1].args)
	}
	_ = next2
}

func TestConnector_ReadPage_Incremental_PartialPage(t *testing.T) {
	db := openTestDB(t)
	// Page size is 5; we return 2 rows ⇒ no more.
	fakeDriverInstance.addRule(
		`FROM "events"`,
		[]string{"id", "ts"},
		[][]driver.Value{
			{int64(1), "a"},
			{int64(2), "b"},
		},
	)
	c, err := NewWithDB(db, Config{
		Driver:            "postgres",
		Table:             "events",
		PageSize:          5,
		IncrementalColumn: "ts",
	})
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}
	_, next, more, err := c.ReadPage(context.Background(), "")
	if err != nil {
		t.Fatalf("ReadPage: %v", err)
	}
	if more {
		t.Errorf("more=true; want false on partial page")
	}
	if next != "" {
		t.Errorf("next=%q want empty when no more", next)
	}
}

func TestConnector_ReadAll(t *testing.T) {
	db := openTestDB(t)
	// Three pages: 2 rows, 2 rows, 1 row (partial).
	calls := 0
	fakeDriverInstance.mu.Lock()
	fakeDriverInstance.rules = append(fakeDriverInstance.rules, responseRule{
		match: `FROM "users"`,
		cols:  []string{"id"},
		rows:  nil, // unused; we'll overwrite via dynamic responder below
	})
	fakeDriverInstance.mu.Unlock()
	// Rather than dynamic responses, simulate by replacing the single rule
	// before each call. We'll do that by clearing and re-adding.
	pages := [][][]driver.Value{
		{{int64(1)}, {int64(2)}},
		{{int64(3)}, {int64(4)}},
		{{int64(5)}},
	}
	c, err := NewWithDB(db, Config{
		Driver:   "postgres",
		Table:    "users",
		PageSize: 2,
		OrderBy:  []string{"id"},
	})
	if err != nil {
		t.Fatalf("NewWithDB: %v", err)
	}
	all := []map[string]any{}
	cursor := ""
	for {
		// Replace canned rule per call.
		fakeDriverInstance.reset()
		fakeDriverInstance.addRule(`FROM "users"`, []string{"id"}, pages[calls])
		calls++
		rows, next, more, err := c.ReadPage(context.Background(), cursor)
		if err != nil {
			t.Fatalf("ReadPage: %v", err)
		}
		all = append(all, rows...)
		if !more {
			break
		}
		cursor = next
	}
	if len(all) != 5 {
		t.Fatalf("want 5 rows total, got %d", len(all))
	}
	if calls != 3 {
		t.Errorf("expected 3 ReadPage calls, got %d", calls)
	}
}

func TestConnector_RejectsBadCursorForOffset(t *testing.T) {
	db := openTestDB(t)
	c, err := NewWithDB(db, Config{
		Driver:   "postgres",
		Table:    "t",
		PageSize: 10,
		OrderBy:  []string{"id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := c.ReadPage(context.Background(), "not-a-number"); err == nil {
		t.Fatal("non-numeric cursor must fail for non-incremental reads")
	}
}

func TestConnector_QueryError(t *testing.T) {
	db := openTestDB(t)
	fakeDriverInstance.queryErr = fmt.Errorf("simulated db failure")
	c, err := NewWithDB(db, Config{
		Driver:   "postgres",
		Table:    "t",
		PageSize: 10,
		OrderBy:  []string{"id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := c.ReadPage(context.Background(), ""); err == nil {
		t.Fatal("expected query error to surface")
	}
}

func TestConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"empty", Config{}, true},
		{"missing-table", Config{Driver: "postgres", PageSize: 10}, true},
		{"bad-table", Config{Driver: "postgres", Table: "bad ident", PageSize: 10}, true},
		{"non-incremental-needs-orderby", Config{Driver: "postgres", Table: "t", PageSize: 10}, true},
		{"good-non-incremental", Config{Driver: "postgres", Table: "t", PageSize: 10, OrderBy: []string{"id"}}, false},
		{"good-incremental", Config{Driver: "postgres", Table: "t", PageSize: 10, IncrementalColumn: "ts"}, false},
		{"bad-pagesize", Config{Driver: "postgres", Table: "t", PageSize: -1, OrderBy: []string{"id"}}, true},
		{"bad-column", Config{Driver: "postgres", Table: "t", Columns: []string{"a; --"}, PageSize: 10, OrderBy: []string{"id"}}, true},
		{"bad-orderby", Config{Driver: "postgres", Table: "t", PageSize: 10, OrderBy: []string{"bad ident"}}, true},
		{"bad-incremental", Config{Driver: "postgres", Table: "t", PageSize: 10, IncrementalColumn: "x; DROP"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate err=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestConfig_DefaultPageSize(t *testing.T) {
	cfg := Config{Driver: "postgres", Table: "t", OrderBy: []string{"id"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.effectivePageSize() != DefaultPageSize {
		t.Errorf("default page size = %d want %d", cfg.effectivePageSize(), DefaultPageSize)
	}
}

func TestConnector_Close(t *testing.T) {
	db := openTestDB(t)
	c, err := NewWithDB(db, Config{
		Driver:   "postgres",
		Table:    "t",
		PageSize: 10,
		OrderBy:  []string{"id"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Close should not error and should close the underlying *sql.DB.
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestNew_OpenDriver(t *testing.T) {
	// Verify New() actually exercises sql.Open with the Driver/DSN fields.
	c, err := New(Config{
		Driver:   "weave-test-pg",
		DSN:      "test://memory",
		Table:    "users",
		PageSize: 10,
		OrderBy:  []string{"id"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if c.db == nil {
		t.Fatal("New must populate db")
	}
}

func TestNew_UnknownDriver(t *testing.T) {
	_, err := New(Config{
		Driver:   "no-such-driver",
		DSN:      "x",
		Table:    "t",
		PageSize: 10,
		OrderBy:  []string{"id"},
	})
	if err == nil {
		t.Fatal("expected error for unknown driver")
	}
}
