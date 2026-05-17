package where

import (
	"encoding/json"
	"testing"
)

// TestBDD_ContainsAnyTerm_AcceptsStringAndArray locks in the DOG-004
// contract: the converter accepts either the canonical string form
// ("OpenAI Codex") or the legacy array form (["OpenAI","Codex"]), and
// both yield equivalent MatchQuery semantics — matching documents that
// contain either term in the target field. The dogfood report captured
// the array form silently rejected with
// `containsAnyTerm value must be a string`, and the browser surfaced
// `INVALID_ARGUMENT: SearchObjectsFailed`.
func TestBDD_ContainsAnyTerm_AcceptsStringAndArray(t *testing.T) {
	idx := setupTestIndex(t)

	// Given a containsAnyTerm with a multi-term STRING value,
	// When converted and executed,
	// Then both "manager" and "engineer" documents match.
	stringClause := &WhereClause{
		Type:  "containsAnyTerm",
		Field: "description",
		Value: json.RawMessage(`"manager engineer"`),
	}
	stringIDs := searchWithWhere(t, idx, stringClause)
	assertIDs(t, stringIDs, []string{"1", "2", "3"})

	// Given the equivalent ARRAY value (legacy frontend serialisation),
	// When converted and executed,
	// Then the result set is identical.
	arrayClause := &WhereClause{
		Type:  "containsAnyTerm",
		Field: "description",
		Value: json.RawMessage(`["manager","engineer"]`),
	}
	arrayIDs := searchWithWhere(t, idx, arrayClause)
	assertIDs(t, arrayIDs, []string{"1", "2", "3"})
}

// TestBDD_ContainsAnyTerm_RejectsNonStringNonArray makes sure we still
// surface a clear error for genuinely malformed values (e.g. numbers,
// objects) — the array tolerance is targeted, not a free-for-all.
func TestBDD_ContainsAnyTerm_RejectsNonStringNonArray(t *testing.T) {
	clause := &WhereClause{
		Type:  "containsAnyTerm",
		Field: "description",
		Value: json.RawMessage(`{"oops":true}`),
	}
	if _, err := ConvertToBleveQuery(clause); err == nil {
		t.Fatal("expected error for object value, got nil")
	}
}
