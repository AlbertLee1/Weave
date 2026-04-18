package where

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2/search/query"
)

func TestConvertRegex_ProducesRegexpQuery(t *testing.T) {
	clause := &WhereClause{
		Type:  "regex",
		Field: "name",
		Value: json.RawMessage(`"^a.*"`),
	}
	q, err := ConvertToBleveQuery(clause)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	rq, ok := q.(*query.RegexpQuery)
	if !ok {
		t.Fatalf("want *query.RegexpQuery, got %T", q)
	}
	if rq.Regexp != "^a.*" {
		t.Fatalf("want regexp=%q, got %q", "^a.*", rq.Regexp)
	}
	if rq.FieldVal != "name" {
		t.Fatalf("want field=name, got %q", rq.FieldVal)
	}
}

func TestConvertRegex_LowercasesPattern(t *testing.T) {
	clause := &WhereClause{
		Type:  "regex",
		Field: "name",
		Value: json.RawMessage(`"^A.*"`),
	}
	q, err := ConvertToBleveQuery(clause)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	rq := q.(*query.RegexpQuery)
	if rq.Regexp != "^a.*" {
		t.Fatalf("want lowercased ^a.*, got %q", rq.Regexp)
	}
}

func TestConvertRegex_AppliesAgainstIndex(t *testing.T) {
	idx := setupTestIndex(t)

	// Bleve's regex engine (vellum FSA) anchors patterns to the FULL indexed
	// term implicitly, so `a.*` matches "alice" (the whole term begins with
	// "a" and the .* consumes the rest). The `^` start anchor is tolerated
	// (treated as a no-op given the implicit anchoring); `$` and other
	// zero-width assertions are rejected by vellum at search time. The
	// converter accepts anchors at parse time so users can write familiar
	// regex syntax — bleve will reject genuinely-unsupported constructs at
	// search time and the OSS handler surfaces that as a 400.
	cases := []struct {
		name     string
		pattern  string
		expected []string
	}{
		{"prefix-a", "a.*", []string{"1"}},          // alice
		{"prefix-b", "b.*", []string{"2"}},          // bob
		{"prefix-c", "c.*", []string{"3"}},          // charlie
		{"middle-li", ".*li.*", []string{"1", "3"}}, // alice, charlie
		{"none", "z.*", nil},                        // no match
		{"uppercase-input", "A.*", []string{"1"}},   // lowercased to a.*
		{"start-anchor", "^a.*", []string{"1"}},     // anchor tolerated
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.pattern)
			got := searchWithWhere(t, idx, &WhereClause{
				Type:  "regex",
				Field: "name",
				Value: payload,
			})
			if len(got) != len(tc.expected) {
				t.Fatalf("want %v, got %v", tc.expected, got)
			}
			for i, id := range got {
				if id != tc.expected[i] {
					t.Fatalf("want %v, got %v", tc.expected, got)
				}
			}
		})
	}
}

func TestConvertRegex_RejectsEmptyValue(t *testing.T) {
	clause := &WhereClause{
		Type:  "regex",
		Field: "name",
		Value: json.RawMessage(`""`),
	}
	if _, err := ConvertToBleveQuery(clause); err == nil {
		t.Fatalf("want error for empty pattern")
	}
}

func TestConvertRegex_RejectsNonString(t *testing.T) {
	clause := &WhereClause{
		Type:  "regex",
		Field: "name",
		Value: json.RawMessage(`123`),
	}
	if _, err := ConvertToBleveQuery(clause); err == nil {
		t.Fatalf("want error for non-string pattern")
	}
}

func TestConvertRegex_RejectsInvalidPattern(t *testing.T) {
	// Unbalanced parenthesis is rejected by regexp/syntax.
	clause := &WhereClause{
		Type:  "regex",
		Field: "name",
		Value: json.RawMessage(`"^(unbalanced"`),
	}
	_, err := ConvertToBleveQuery(clause)
	if err == nil {
		t.Fatalf("want error for invalid pattern")
	}
	if !strings.Contains(err.Error(), "regex pattern invalid") {
		t.Fatalf("want 'regex pattern invalid' in error, got %v", err)
	}
}

func TestConvertRegex_RejectsOverlongPattern(t *testing.T) {
	pattern := strings.Repeat("a", MaxRegexPatternLength+1)
	payload, _ := json.Marshal(pattern)
	clause := &WhereClause{
		Type:  "regex",
		Field: "name",
		Value: payload,
	}
	_, err := ConvertToBleveQuery(clause)
	if err == nil {
		t.Fatalf("want error for overlong pattern")
	}
	if !strings.Contains(err.Error(), "exceeds max") {
		t.Fatalf("want length error, got %v", err)
	}
}

func TestHasRegexClause(t *testing.T) {
	regex := &WhereClause{
		Type:  "regex",
		Field: "name",
		Value: json.RawMessage(`"^a.*"`),
	}
	eq := &WhereClause{
		Type:  "eq",
		Field: "name",
		Value: json.RawMessage(`"alice"`),
	}

	if !HasRegexClause(regex) {
		t.Fatal("want true for direct regex clause")
	}
	if HasRegexClause(nil) {
		t.Fatal("want false for nil clause")
	}
	if HasRegexClause(eq) {
		t.Fatal("want false for non-regex clause")
	}

	// AND containing regex
	andSubs, _ := json.Marshal([]*WhereClause{eq, regex})
	andClause := &WhereClause{Type: "and", Value: andSubs}
	if !HasRegexClause(andClause) {
		t.Fatal("want true for and(eq, regex)")
	}

	// OR containing regex
	orSubs, _ := json.Marshal([]*WhereClause{regex, eq})
	orClause := &WhereClause{Type: "or", Value: orSubs}
	if !HasRegexClause(orClause) {
		t.Fatal("want true for or(regex, eq)")
	}

	// NOT object form
	notSingle, _ := json.Marshal(regex)
	notClause := &WhereClause{Type: "not", Value: notSingle}
	if !HasRegexClause(notClause) {
		t.Fatal("want true for not(regex) as object")
	}

	// NOT array form
	notArr, _ := json.Marshal([]*WhereClause{regex})
	notArrClause := &WhereClause{Type: "not", Value: notArr}
	if !HasRegexClause(notArrClause) {
		t.Fatal("want true for not([regex]) as array")
	}

	// AND of only non-regex clauses
	plainAnd, _ := json.Marshal([]*WhereClause{eq, eq})
	if HasRegexClause(&WhereClause{Type: "and", Value: plainAnd}) {
		t.Fatal("want false for and(eq, eq)")
	}
}
