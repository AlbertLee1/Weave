package jdbc

import (
	"strings"
	"testing"
)

func TestDialect_Quote(t *testing.T) {
	cases := []struct {
		name    string
		dialect Dialect
		ident   string
		want    string
	}{
		{"postgres-bare", DialectPostgres, "users", `"users"`},
		{"postgres-schema", DialectPostgres, "public.users", `"public"."users"`},
		{"mysql-bare", DialectMySQL, "users", "`users`"},
		{"mysql-schema", DialectMySQL, "shop.orders", "`shop`.`orders`"},
		{"sqlite-bare", DialectSQLite, "users", `"users"`},
		{"sqlite-schema", DialectSQLite, "main.t", `"main"."t"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.dialect.Quote(tc.ident)
			if got != tc.want {
				t.Fatalf("%s.Quote(%q) = %q want %q", tc.dialect, tc.ident, got, tc.want)
			}
		})
	}
}

func TestDialect_Placeholder(t *testing.T) {
	if got := DialectPostgres.Placeholder(1); got != "$1" {
		t.Fatalf("postgres placeholder: %q", got)
	}
	if got := DialectPostgres.Placeholder(7); got != "$7" {
		t.Fatalf("postgres placeholder: %q", got)
	}
	if got := DialectMySQL.Placeholder(1); got != "?" {
		t.Fatalf("mysql placeholder: %q", got)
	}
	if got := DialectSQLite.Placeholder(1); got != "?" {
		t.Fatalf("sqlite placeholder: %q", got)
	}
}

func TestDialectFromDriver(t *testing.T) {
	cases := map[string]Dialect{
		"postgres":   DialectPostgres,
		"postgresql": DialectPostgres,
		"pgx":        DialectPostgres,
		"pq":         DialectPostgres,
		"mysql":      DialectMySQL,
		"sqlite":     DialectSQLite,
		"sqlite3":    DialectSQLite,
	}
	for driverName, want := range cases {
		t.Run(driverName, func(t *testing.T) {
			got, err := DialectFromDriver(driverName)
			if err != nil {
				t.Fatalf("DialectFromDriver(%q) err: %v", driverName, err)
			}
			if got != want {
				t.Fatalf("DialectFromDriver(%q) = %q want %q", driverName, got, want)
			}
		})
	}

	if _, err := DialectFromDriver(""); err == nil {
		t.Fatal("empty driver should error")
	}
	if _, err := DialectFromDriver("oracle"); err == nil {
		t.Fatal("unknown driver should error")
	}
}

func TestValidateIdentifier(t *testing.T) {
	good := []string{"users", "user_orders", "_temp", "T1", "schema.table"}
	for _, id := range good {
		if err := ValidateIdentifier(id); err != nil {
			t.Errorf("ValidateIdentifier(%q) unexpected err: %v", id, err)
		}
	}
	bad := []string{
		"",
		"users; DROP TABLE x",
		"users--x",
		"users x",
		"users.table.extra",
		".users",
		"users.",
		"`users`",
		`"users"`,
		"users\x00",
	}
	for _, id := range bad {
		if err := ValidateIdentifier(id); err == nil {
			t.Errorf("ValidateIdentifier(%q) should have rejected", id)
		}
	}
}

func TestBuildSelect_NoIncremental(t *testing.T) {
	cfg := Config{
		Table:    "users",
		Columns:  []string{"id", "name"},
		PageSize: 100,
		OrderBy:  []string{"id"},
	}
	sql, args, err := buildSelectSQL(DialectPostgres, cfg, "", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantSubs := []string{
		`SELECT "id", "name" FROM "users"`,
		`ORDER BY "id" ASC`,
		`LIMIT 100`,
	}
	for _, w := range wantSubs {
		if !strings.Contains(sql, w) {
			t.Errorf("SQL %q missing %q", sql, w)
		}
	}
	if len(args) != 0 {
		t.Errorf("non-incremental: args=%v want empty", args)
	}
	if !strings.Contains(sql, "OFFSET 0") {
		t.Errorf("first page missing OFFSET 0: %q", sql)
	}
}

func TestBuildSelect_Incremental(t *testing.T) {
	cfg := Config{
		Table:             "events",
		PageSize:          50,
		IncrementalColumn: "ts",
	}
	// First page: no cursor.
	sql, args, err := buildSelectSQL(DialectPostgres, cfg, "", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(sql, `SELECT * FROM "events"`) {
		t.Errorf("missing select-all: %q", sql)
	}
	if !strings.Contains(sql, `ORDER BY "ts" ASC`) {
		t.Errorf("missing order by: %q", sql)
	}
	if !strings.Contains(sql, "LIMIT 50") {
		t.Errorf("missing limit: %q", sql)
	}
	if len(args) != 0 {
		t.Errorf("first page args=%v want empty", args)
	}
	// Second page: with cursor.
	sql, args, err = buildSelectSQL(DialectPostgres, cfg, "2024-01-01", 0)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(sql, `WHERE "ts" > $1`) {
		t.Errorf("missing watermark predicate: %q", sql)
	}
	if len(args) != 1 || args[0] != "2024-01-01" {
		t.Errorf("args=%v want [2024-01-01]", args)
	}
}

func TestBuildSelect_RejectsBadIdentifier(t *testing.T) {
	bad := Config{Table: "users; DROP TABLE x", PageSize: 10}
	if _, _, err := buildSelectSQL(DialectPostgres, bad, "", 0); err == nil {
		t.Fatal("bad table should error")
	}
	bad2 := Config{Table: "users", Columns: []string{"id; --"}, PageSize: 10}
	if _, _, err := buildSelectSQL(DialectPostgres, bad2, "", 0); err == nil {
		t.Fatal("bad column should error")
	}
	bad3 := Config{Table: "users", PageSize: 10, IncrementalColumn: "ts; DROP"}
	if _, _, err := buildSelectSQL(DialectPostgres, bad3, "", 0); err == nil {
		t.Fatal("bad incremental column should error")
	}
	bad4 := Config{Table: "users", PageSize: 10, OrderBy: []string{"ok", "bad ident"}}
	if _, _, err := buildSelectSQL(DialectPostgres, bad4, "", 0); err == nil {
		t.Fatal("bad order-by should error")
	}
}
