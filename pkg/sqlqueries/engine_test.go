package sqlqueries_test

import (
	"testing"

	"github.com/liyang/weave/pkg/sqlqueries"
)

func TestIsSelectQuery(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  bool
	}{
		{"plain select", "SELECT 1", true},
		{"lowercase select", "select * from foo", true},
		{"leading whitespace", "   \n\tSELECT id FROM bar", true},
		{"with cte then select", "WITH t AS (SELECT 1) SELECT * FROM t", true},
		{"trailing semicolon ok", "SELECT 1;", true},
		{"trailing semicolon and whitespace ok", "SELECT 1;  \n", true},
		{"empty rejected", "", false},
		{"whitespace only rejected", "   \n  ", false},
		{"insert rejected", "INSERT INTO foo VALUES (1)", false},
		{"update rejected", "UPDATE foo SET x=1", false},
		{"delete rejected", "DELETE FROM foo", false},
		{"drop rejected", "DROP TABLE foo", false},
		{"stacked statement rejected", "SELECT 1; DROP TABLE users", false},
		{"truncate rejected", "TRUNCATE foo", false},
		{"select-prefixed identifier rejected", "SELECTOR 1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sqlqueries.IsSelectQuery(tc.query)
			if got != tc.want {
				t.Fatalf("IsSelectQuery(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}
