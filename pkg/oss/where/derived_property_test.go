package where

import (
	"encoding/json"
	"errors"
	"testing"
)

// TestDerivedPropertyConstraints_TextSearch covers US-004 part 2: text-search
// style clauses must refuse to run against derived (computed) properties. Only
// the six text search operators (contains, containsAllTerms, containsAnyTerm,
// containsAllTermsInOrder, startsWith, wildcard) are blocked; numeric and
// equality clauses are unaffected because those already fall through to the
// (still error-prone) default code path that derived properties won't match.
func TestDerivedPropertyConstraints_TextSearch(t *testing.T) {
	derived := map[string]bool{"orderCount": true}

	textSearchOps := []string{
		"contains",
		"containsAllTerms",
		"containsAnyTerm",
		"containsAllTermsInOrder",
		"startsWith",
		"wildcard",
	}

	for _, op := range textSearchOps {
		op := op
		t.Run(op+" on derived field is rejected", func(t *testing.T) {
			clause := &WhereClause{
				Type:  op,
				Field: "orderCount",
				Value: json.RawMessage(`"foo"`),
			}
			err := ValidateClauseAgainstDerivedFields(clause, derived)
			if err == nil {
				t.Fatalf("expected error for %s on derived field", op)
			}
			if !errors.Is(err, ErrDerivedPropertyTextSearchUnsupported) {
				t.Fatalf("expected ErrDerivedPropertyTextSearchUnsupported, got %v", err)
			}
		})
	}

	t.Run("text search on non-derived field is allowed", func(t *testing.T) {
		clause := &WhereClause{
			Type:  "contains",
			Field: "name",
			Value: json.RawMessage(`"alice"`),
		}
		if err := ValidateClauseAgainstDerivedFields(clause, derived); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("equality operator on derived field is not blocked here", func(t *testing.T) {
		clause := &WhereClause{
			Type:  "eq",
			Field: "orderCount",
			Value: json.RawMessage(`3`),
		}
		if err := ValidateClauseAgainstDerivedFields(clause, derived); err != nil {
			t.Fatalf("unexpected error for eq on derived field: %v", err)
		}
	})

	t.Run("and clause recurses into subclauses", func(t *testing.T) {
		inner1 := WhereClause{Type: "contains", Field: "orderCount", Value: json.RawMessage(`"3"`)}
		inner2 := WhereClause{Type: "eq", Field: "name", Value: json.RawMessage(`"alice"`)}
		subs, _ := json.Marshal([]WhereClause{inner1, inner2})
		outer := &WhereClause{Type: "and", Value: subs}
		err := ValidateClauseAgainstDerivedFields(outer, derived)
		if err == nil || !errors.Is(err, ErrDerivedPropertyTextSearchUnsupported) {
			t.Fatalf("expected nested derived-text-search rejection, got %v", err)
		}
	})

	t.Run("nil derived map is a no-op", func(t *testing.T) {
		clause := &WhereClause{Type: "contains", Field: "orderCount", Value: json.RawMessage(`"foo"`)}
		if err := ValidateClauseAgainstDerivedFields(clause, nil); err != nil {
			t.Fatalf("unexpected error with nil derived map: %v", err)
		}
	})
}
