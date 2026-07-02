package where

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// --- "in" operator tests (Foundry SearchJsonQueryV2 parity) ---
//
// Foundry semantics: {"type":"in","field":"<prop>","value":[v1,v2,...]}
// matches objects whose field equals ANY value in the array — the exact
// equivalent of OR-ing one "eq" clause per element. Each element must go
// through the same type handling as "eq" (number → numeric range,
// boolean → bool field, string → analyzer-consistent MatchQuery).

func TestIn_MatchesAnyValueInArray(t *testing.T) {
	idx := setupTestIndex(t)

	tests := []struct {
		name  string
		field string
		value string
		want  []string
	}{
		{
			name:  "string array matches union of per-element eq hits",
			field: "name",
			value: `["alice","bob"]`,
			want:  []string{"1", "2"},
		},
		{
			name:  "string array is analyzed like eq (case-insensitive match)",
			field: "name",
			value: `["Alice"]`,
			want:  []string{"1"},
		},
		{
			name:  "numeric array matches exact numeric values",
			field: "age",
			value: `[25, 35]`,
			want:  []string{"2", "3"},
		},
		{
			name:  "boolean array matches bool field values",
			field: "active",
			value: `[false]`,
			want:  []string{"2"},
		},
		{
			name:  "single element behaves exactly like eq",
			field: "name",
			value: `["charlie"]`,
			want:  []string{"3"},
		},
		{
			name:  "no candidate matches yields zero hits",
			field: "name",
			value: `["nobody","nemo"]`,
			want:  []string{},
		},
		{
			name:  "empty array is legal and matches zero objects",
			field: "name",
			value: `[]`,
			want:  []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clause := &WhereClause{
				Type:  "in",
				Field: tc.field,
				Value: json.RawMessage(tc.value),
			}
			ids := searchWithWhere(t, idx, clause)
			assertIDs(t, ids, tc.want)
		})
	}
}

func TestIn_EquivalentToOrOfEq(t *testing.T) {
	// The defining contract: `in` over [v1, v2] returns the identical
	// result set as or(eq v1, eq v2).
	idx := setupTestIndex(t)

	inClause := &WhereClause{
		Type:  "in",
		Field: "name",
		Value: json.RawMessage(`["alice","charlie"]`),
	}
	orClause := &WhereClause{
		Type: "or",
		Value: json.RawMessage(`[
			{"type": "eq", "field": "name", "value": "alice"},
			{"type": "eq", "field": "name", "value": "charlie"}
		]`),
	}

	inIDs := searchWithWhere(t, idx, inClause)
	orIDs := searchWithWhere(t, idx, orClause)
	assertIDs(t, inIDs, []string{"1", "3"})
	assertIDs(t, orIDs, inIDs)
}

func TestIn_FuzzyOptsApplyPerElementLikeEq(t *testing.T) {
	// eq threads the request-level fuzzy option into its MatchQuery; `in`
	// converts each element with the same code path, so the option must
	// behave identically per element.
	idx := setupFuzzyTestIndex(t)
	clause := &WhereClause{
		Type:  "in",
		Field: "name",
		Value: json.RawMessage(`["Jonh"]`),
	}

	// Without fuzzy: the typo misses.
	assertIDs(t, searchWithWhere(t, idx, clause), []string{})

	// With fuzzy maxEdits=1: matches "John Smith" exactly like eq does.
	opts := &ConvertOptions{Fuzzy: &FuzzyConfig{MaxEdits: 1}}
	assertIDs(t, searchWithWhereOpts(t, idx, clause, opts), []string{"1"})
}

func TestIn_InvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantSub string
	}{
		{
			name:    "non-array string value rejected",
			value:   `"alice"`,
			wantSub: "in value must be an array",
		},
		{
			name:    "non-array number value rejected",
			value:   `42`,
			wantSub: "in value must be an array",
		},
		{
			name:    "non-array object value rejected",
			value:   `{"oops":true}`,
			wantSub: "in value must be an array",
		},
		{
			name:    "null value rejected",
			value:   `null`,
			wantSub: "in value must be an array",
		},
		{
			name:    "unsupported element type rejected with element index",
			value:   `[{"nested":true}]`,
			wantSub: "in value[0]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clause := &WhereClause{
				Type:  "in",
				Field: "name",
				Value: json.RawMessage(tc.value),
			}
			_, err := ConvertToBleveQuery(clause)
			if err == nil {
				t.Fatalf("expected error for value %s, got nil", tc.value)
			}
			if !errors.Is(err, ErrInvalidWhereClause) {
				t.Errorf("error %v must wrap ErrInvalidWhereClause so the handler returns 400", err)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q must mention %q", err.Error(), tc.wantSub)
			}
		})
	}
}
