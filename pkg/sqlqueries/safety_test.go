package sqlqueries_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/sqlqueries"
)

// TestValidateQuery_PositiveCases pins the queries that MUST pass
// safety. Anything in this list is a vanilla read-only statement that
// the SqlQueries.execute endpoint should accept.
func TestValidateQuery_PositiveCases(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"plain select", "SELECT 1"},
		{"select with whitespace", "  \n\t SELECT 1  "},
		{"select with table", "SELECT id FROM users"},
		{"lowercase select", "select * from users"},
		{"mixed case select", "SeLect id FROM users"},
		{"with cte then select", "WITH t AS (SELECT 1) SELECT * FROM t"},
		{"trailing semicolon", "SELECT 1;"},
		{"trailing semicolon and whitespace", "SELECT 1;  \n"},
		{"line comment then select", "-- comment\nSELECT 1"},
		{"block comment then select", "/* comment */ SELECT 1"},
		{"block comment in middle", "SELECT /* comment */ 1"},
		{"select with quoted identifier", `SELECT "myCol" FROM "myTable"`},
		{"select with numeric literal", "SELECT 1 + 2"},
		{"select with string literal", "SELECT 'hello'"},
		{"select with escaped quote literal", "SELECT 'it''s ok'"},
		{"values statement", "VALUES (1), (2), (3)"},
		{"table command", "TABLE users"},
		{"select with join", "SELECT u.id FROM users u JOIN orders o ON o.user_id = u.id"},
		{"select with subquery", "SELECT * FROM (SELECT 1 AS x) sub"},
		{"select with where clause", "SELECT * FROM users WHERE name = 'alice'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := sqlqueries.ValidateQuery(tc.query); err != nil {
				t.Fatalf("ValidateQuery(%q) = %v, want nil", tc.query, err)
			}
		})
	}
}

// TestValidateQuery_NegativeCases covers the 20+ payloads called for
// by US-434 acceptance criteria. Each case asserts the specific
// sentinel — a future change to validator wording must keep the
// errors.Is verdict stable.
func TestValidateQuery_NegativeCases(t *testing.T) {
	cases := []struct {
		name        string
		query       string
		wantSentinel error
	}{
		// DML rejections.
		{"insert", "INSERT INTO users (id) VALUES (1)", sqlqueries.ErrForbiddenStatement},
		{"update", "UPDATE users SET name = 'x' WHERE id = 1", sqlqueries.ErrForbiddenStatement},
		{"delete", "DELETE FROM users WHERE id = 1", sqlqueries.ErrForbiddenStatement},
		{"merge", "MERGE INTO users USING staging ON users.id = staging.id WHEN MATCHED THEN UPDATE SET name = staging.name", sqlqueries.ErrForbiddenStatement},
		{"replace", "REPLACE INTO users (id, name) VALUES (1, 'a')", sqlqueries.ErrForbiddenStatement},
		// DDL rejections.
		{"drop table", "DROP TABLE users", sqlqueries.ErrForbiddenStatement},
		{"alter table", "ALTER TABLE users ADD COLUMN x int", sqlqueries.ErrForbiddenStatement},
		{"truncate", "TRUNCATE users", sqlqueries.ErrForbiddenStatement},
		{"create table", "CREATE TABLE foo (id int)", sqlqueries.ErrForbiddenStatement},
		{"create index", "CREATE INDEX idx ON users(name)", sqlqueries.ErrForbiddenStatement},
		// DCL rejections.
		{"grant", "GRANT SELECT ON users TO bob", sqlqueries.ErrForbiddenStatement},
		{"revoke", "REVOKE SELECT ON users FROM bob", sqlqueries.ErrForbiddenStatement},
		// Stacked statements (semicolon between statements).
		{"stacked select then drop", "SELECT 1; DROP TABLE users", sqlqueries.ErrStackedStatement},
		{"stacked select then insert", "SELECT 1; INSERT INTO users VALUES (1)", sqlqueries.ErrStackedStatement},
		{"stacked select then select", "SELECT 1; SELECT 2", sqlqueries.ErrStackedStatement},
		// System-table refs.
		{"pg_user direct", "SELECT * FROM pg_user", sqlqueries.ErrSystemTableAccess},
		{"pg_class direct", "SELECT relname FROM pg_class", sqlqueries.ErrSystemTableAccess},
		{"pg_catalog qualified", "SELECT * FROM pg_catalog.pg_class", sqlqueries.ErrSystemTableAccess},
		{"information_schema qualified", "SELECT * FROM information_schema.tables", sqlqueries.ErrSystemTableAccess},
		{"information_schema columns", "SELECT * FROM information_schema.columns WHERE table_name = 'users'", sqlqueries.ErrSystemTableAccess},
		{"pg_stat_activity", "SELECT * FROM pg_stat_activity", sqlqueries.ErrSystemTableAccess},
		// Empty / malformed.
		{"empty", "", sqlqueries.ErrEmptyQuery},
		{"only whitespace", "   \n  \t  ", sqlqueries.ErrEmptyQuery},
		{"only comment", "-- nothing here", sqlqueries.ErrEmptyQuery},
		{"only block comment", "/* still nothing */", sqlqueries.ErrEmptyQuery},
		// Other forbidden kinds.
		{"vacuum", "VACUUM users", sqlqueries.ErrForbiddenStatement},
		{"copy", "COPY users TO '/tmp/users.csv'", sqlqueries.ErrForbiddenStatement},
		{"set", "SET search_path = public", sqlqueries.ErrForbiddenStatement},
		{"begin transaction", "BEGIN", sqlqueries.ErrForbiddenStatement},
		{"commit", "COMMIT", sqlqueries.ErrForbiddenStatement},
		{"rollback", "ROLLBACK", sqlqueries.ErrForbiddenStatement},
		// Embedded mutating CTE.
		{"with delete cte", "WITH d AS (DELETE FROM users RETURNING id) SELECT * FROM d", sqlqueries.ErrForbiddenStatement},
		{"with update cte", "WITH u AS (UPDATE users SET name = 'x' RETURNING id) SELECT * FROM u", sqlqueries.ErrForbiddenStatement},
		// Stacked under a dressed-up disguise (comment-prefixed statement).
		{"comment then drop", "/* harmless */ DROP TABLE users", sqlqueries.ErrForbiddenStatement},
		{"line comment then update", "-- comment\nUPDATE users SET x = 1", sqlqueries.ErrForbiddenStatement},
	}
	if len(cases) < 20 {
		t.Fatalf("US-434 requires 20+ negative payloads, have %d", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sqlqueries.ValidateQuery(tc.query)
			if err == nil {
				t.Fatalf("ValidateQuery(%q) = nil, want error %v", tc.query, tc.wantSentinel)
			}
			if !errors.Is(err, tc.wantSentinel) {
				t.Fatalf("ValidateQuery(%q) = %v, want errors.Is sentinel %v", tc.query, err, tc.wantSentinel)
			}
		})
	}
}

// TestValidateQuery_DoesNotMatchInsideStringLiteral asserts that a
// forbidden keyword sitting inside a string literal does NOT trigger a
// false-positive rejection. This is the key reason we tokenise instead
// of doing substring matches.
func TestValidateQuery_DoesNotMatchInsideStringLiteral(t *testing.T) {
	cases := []string{
		"SELECT 'DROP TABLE users'",
		"SELECT 'INSERT INTO foo'",
		"SELECT 'pg_user'",
		"SELECT 'information_schema.tables'",
		"SELECT name FROM users WHERE name = 'pg_admin'",
		"SELECT name FROM users WHERE name = 'it''s a DROP TABLE attempt'",
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			if err := sqlqueries.ValidateQuery(q); err != nil {
				t.Fatalf("ValidateQuery(%q) = %v, want nil (string literal must not trigger reject)", q, err)
			}
		})
	}
}

// TestValidateQuery_DoesNotMatchInsideComment confirms that comments do
// not contribute keywords to validation.
func TestValidateQuery_DoesNotMatchInsideComment(t *testing.T) {
	cases := []string{
		"SELECT /* DROP TABLE users */ 1",
		"SELECT /* pg_user */ 1",
		"SELECT 1 -- DROP TABLE users",
		"SELECT 1 -- pg_class lives here",
		"SELECT 1 /* nested /* DROP */ comment */",
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			if err := sqlqueries.ValidateQuery(q); err != nil {
				t.Fatalf("ValidateQuery(%q) = %v, want nil (comment must not trigger reject)", q, err)
			}
		})
	}
}

// TestValidateQuery_QuotedIdentifiers_TrustedNames keeps the verdict
// honest for valid PostgreSQL quoted identifiers that happen to contain
// words like "select" — these are legal column names.
func TestValidateQuery_QuotedIdentifiers_TrustedNames(t *testing.T) {
	cases := []string{
		`SELECT "DROP" FROM users`,
		`SELECT "select" FROM users`,
		`SELECT id FROM "users"`,
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			if err := sqlqueries.ValidateQuery(q); err != nil {
				t.Fatalf("ValidateQuery(%q) = %v, want nil (quoted identifier should be opaque)", q, err)
			}
		})
	}
}

// TestValidateQuery_QuotedIdentifiers_SystemNames asserts that quoting
// a system schema does NOT bypass the system-table reject — the
// identifier match is case-insensitive on the unquoted bytes.
func TestValidateQuery_QuotedIdentifiers_SystemNames(t *testing.T) {
	cases := []string{
		`SELECT * FROM "pg_catalog"."pg_class"`,
		`SELECT * FROM "information_schema"."tables"`,
		`SELECT * FROM "pg_user"`,
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			err := sqlqueries.ValidateQuery(q)
			if err == nil {
				t.Fatalf("ValidateQuery(%q) = nil, want SystemTableAccess error", q)
			}
			if !errors.Is(err, sqlqueries.ErrSystemTableAccess) {
				t.Fatalf("ValidateQuery(%q) = %v, want SystemTableAccess sentinel", q, err)
			}
		})
	}
}

// TestValidateQuery_DollarQuotedString covers PostgreSQL's
// $$...$$ literal syntax which the tokenizer must skip cleanly.
func TestValidateQuery_DollarQuotedString(t *testing.T) {
	cases := []struct {
		name  string
		query string
		ok    bool
	}{
		{"empty tag", "SELECT $$DROP TABLE users$$", true},
		{"named tag", "SELECT $tag$pg_user is not a table here$tag$", true},
		{"unterminated", "SELECT $$DROP TABLE users", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := sqlqueries.ValidateQuery(tc.query)
			if tc.ok && err != nil {
				t.Fatalf("ValidateQuery(%q) = %v, want nil", tc.query, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("ValidateQuery(%q) = nil, want error", tc.query)
			}
		})
	}
}

// TestValidateQuery_StackedThroughComment ensures comment-stripping
// does not let a stacked statement slip past the semicolon guard.
func TestValidateQuery_StackedThroughComment(t *testing.T) {
	q := "SELECT 1 /* harmless */; DROP TABLE users"
	err := sqlqueries.ValidateQuery(q)
	if err == nil {
		t.Fatalf("ValidateQuery(%q) = nil, want StackedStatement", q)
	}
	if !errors.Is(err, sqlqueries.ErrStackedStatement) {
		t.Fatalf("ValidateQuery(%q) = %v, want StackedStatement", q, err)
	}
}

// TestValidateQuery_SystemTableErrorMessageNotConfusing asserts the
// error string carries the offending identifier so the SDK / SPA can
// surface it to the user.
func TestValidateQuery_SystemTableErrorMessageNotConfusing(t *testing.T) {
	q := "SELECT * FROM pg_stat_activity"
	err := sqlqueries.ValidateQuery(q)
	if err == nil {
		t.Fatalf("ValidateQuery(%q) = nil, want error", q)
	}
	if !strings.Contains(err.Error(), "pg_stat_activity") {
		t.Fatalf("ValidateQuery(%q).Error() = %q, want offending identifier in message", q, err.Error())
	}
}
