package where

import (
	"encoding/json"
	"testing"
)

// TestMatchClause_Eq covers the eq operator across string / number / bool
// value shapes. This evaluator is the in-memory companion to the Bleve
// converter — SSE subscribe (US-056) needs to apply an ObjectSet's Where
// clause to a single BroadcastEvent row WITHOUT indexing the row first.
func TestMatchClause_Eq(t *testing.T) {
	cases := []struct {
		name   string
		clause string
		row    map[string]interface{}
		want   bool
	}{
		{
			name:   "string equal",
			clause: `{"type":"eq","field":"status","value":"SHIPPED"}`,
			row:    map[string]interface{}{"status": "SHIPPED"},
			want:   true,
		},
		{
			name:   "string not equal",
			clause: `{"type":"eq","field":"status","value":"SHIPPED"}`,
			row:    map[string]interface{}{"status": "PENDING"},
			want:   false,
		},
		{
			name:   "string missing field",
			clause: `{"type":"eq","field":"status","value":"SHIPPED"}`,
			row:    map[string]interface{}{"other": "SHIPPED"},
			want:   false,
		},
		{
			name:   "numeric equal int",
			clause: `{"type":"eq","field":"age","value":30}`,
			row:    map[string]interface{}{"age": 30},
			want:   true,
		},
		{
			name:   "numeric equal float",
			clause: `{"type":"eq","field":"age","value":30}`,
			row:    map[string]interface{}{"age": 30.0},
			want:   true,
		},
		{
			name:   "numeric not equal",
			clause: `{"type":"eq","field":"age","value":30}`,
			row:    map[string]interface{}{"age": 31},
			want:   false,
		},
		{
			name:   "bool equal",
			clause: `{"type":"eq","field":"active","value":true}`,
			row:    map[string]interface{}{"active": true},
			want:   true,
		},
		{
			name:   "bool not equal",
			clause: `{"type":"eq","field":"active","value":true}`,
			row:    map[string]interface{}{"active": false},
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c WhereClause
			if err := json.Unmarshal([]byte(tc.clause), &c); err != nil {
				t.Fatalf("unmarshal clause: %v", err)
			}
			got := MatchClause(&c, tc.row)
			if got != tc.want {
				t.Errorf("MatchClause = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMatchClause_Range covers gt/gte/lt/lte for numeric values, which is
// what the Browser-page realtime filter will typically use (e.g. amount > 100).
func TestMatchClause_Range(t *testing.T) {
	row := map[string]interface{}{"amount": 100.0}
	cases := []struct {
		name   string
		clause string
		want   bool
	}{
		{"gt true", `{"type":"gt","field":"amount","value":50}`, true},
		{"gt false", `{"type":"gt","field":"amount","value":100}`, false},
		{"gte equal", `{"type":"gte","field":"amount","value":100}`, true},
		{"lt true", `{"type":"lt","field":"amount","value":200}`, true},
		{"lt false", `{"type":"lt","field":"amount","value":100}`, false},
		{"lte equal", `{"type":"lte","field":"amount","value":100}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c WhereClause
			if err := json.Unmarshal([]byte(tc.clause), &c); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := MatchClause(&c, row); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// TestMatchClause_Logical exercises and/or/not — the SSE filter only makes
// sense once these tree operators compose correctly.
func TestMatchClause_Logical(t *testing.T) {
	row := map[string]interface{}{
		"status": "SHIPPED",
		"amount": 150.0,
		"region": "EU",
	}

	cases := []struct {
		name   string
		clause string
		want   bool
	}{
		{
			name:   "and true",
			clause: `{"type":"and","value":[{"type":"eq","field":"status","value":"SHIPPED"},{"type":"gt","field":"amount","value":100}]}`,
			want:   true,
		},
		{
			name:   "and false",
			clause: `{"type":"and","value":[{"type":"eq","field":"status","value":"SHIPPED"},{"type":"gt","field":"amount","value":200}]}`,
			want:   false,
		},
		{
			name:   "or true",
			clause: `{"type":"or","value":[{"type":"eq","field":"region","value":"APAC"},{"type":"eq","field":"region","value":"EU"}]}`,
			want:   true,
		},
		{
			name:   "or false",
			clause: `{"type":"or","value":[{"type":"eq","field":"region","value":"APAC"},{"type":"eq","field":"region","value":"NA"}]}`,
			want:   false,
		},
		{
			name:   "not object form",
			clause: `{"type":"not","value":{"type":"eq","field":"status","value":"PENDING"}}`,
			want:   true,
		},
		{
			name:   "not array form",
			clause: `{"type":"not","value":[{"type":"eq","field":"status","value":"SHIPPED"}]}`,
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c WhereClause
			if err := json.Unmarshal([]byte(tc.clause), &c); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := MatchClause(&c, row); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

// TestMatchClause_StringOps covers contains / startsWith / isNull — the
// evaluator is case-insensitive for both contains and startsWith to line up
// with the Bleve analyzer path.
func TestMatchClause_StringOps(t *testing.T) {
	row := map[string]interface{}{
		"name":        "Alice in Wonderland",
		"description": "senior engineer",
		"nickname":    nil,
	}
	cases := []struct {
		name   string
		clause string
		want   bool
	}{
		{"contains match", `{"type":"contains","field":"description","value":"engineer"}`, true},
		{"contains miss", `{"type":"contains","field":"description","value":"manager"}`, false},
		{"contains case insensitive", `{"type":"contains","field":"name","value":"wonderland"}`, true},
		{"startsWith match", `{"type":"startsWith","field":"name","value":"Alice"}`, true},
		{"startsWith miss", `{"type":"startsWith","field":"name","value":"Bob"}`, false},
		{"startsWith case insensitive", `{"type":"startsWith","field":"name","value":"alice"}`, true},
		{"isNull true on missing", `{"type":"isNull","field":"missing","value":true}`, true},
		{"isNull true on nil", `{"type":"isNull","field":"nickname","value":true}`, true},
		{"isNull true on present", `{"type":"isNull","field":"name","value":true}`, false},
		{"isNull false on present", `{"type":"isNull","field":"name","value":false}`, true},
		{"isNull false on missing", `{"type":"isNull","field":"missing","value":false}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c WhereClause
			if err := json.Unmarshal([]byte(tc.clause), &c); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := MatchClause(&c, row); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestBDD_SELF606_MatchClauseContainsAnyTermUsesTokenSemanticsAndTextArrays(t *testing.T) {
	cases := []struct {
		name string
		row  map[string]interface{}
		want bool
	}{
		{
			name: "substring inside larger token does not match",
			row:  map[string]interface{}{"notes": "insurgent queue"},
			want: false,
		},
		{
			name: "scalar string token matches case insensitively",
			row:  map[string]interface{}{"notes": "URGENT queue"},
			want: true,
		},
		{
			name: "string array token matches any element",
			row:  map[string]interface{}{"notes": []string{"routine", "urgent review"}},
			want: true,
		},
		{
			name: "json-decoded text array token matches any element",
			row:  map[string]interface{}{"notes": []interface{}{"routine", "urgent review"}},
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var c WhereClause
			if err := json.Unmarshal([]byte(`{"type":"containsAnyTerm","field":"notes","value":"urgent"}`), &c); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := MatchClause(&c, tc.row); got != tc.want {
				t.Errorf("MatchClause = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMatchClause_NilTree — a nil clause is treated as "match everything" so
// an SSE subscription without a Where keeps streaming all events.
func TestMatchClause_NilTree(t *testing.T) {
	if !MatchClause(nil, map[string]interface{}{"any": "row"}) {
		t.Fatalf("nil clause should match")
	}
}
