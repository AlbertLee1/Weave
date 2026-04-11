package where

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrDerivedPropertyTextSearchUnsupported is returned when a full-text search
// style clause targets a derived (computed at query time) property. Derived
// properties have no inverted-index entry so no text-search operator can
// possibly match them; rejecting early gives SDK users a clear diagnostic
// rather than a silent empty result. Error name matches Foundry convention.
var ErrDerivedPropertyTextSearchUnsupported = errors.New("DerivedPropertyTextSearchUnsupported: text search is not supported on derived properties")

// textSearchClauseTypes enumerates the WhereClause.Type values that perform
// text-search style matching and therefore require a proper tokenised index.
var textSearchClauseTypes = map[string]bool{
	"contains":                true,
	"containsAllTerms":        true,
	"containsAnyTerm":         true,
	"containsAllTermsInOrder": true,
	"startsWith":              true,
	"wildcard":                true,
}

// ValidateClauseAgainstDerivedFields walks a WhereClause tree and returns an
// ErrDerivedPropertyTextSearchUnsupported (wrapped with the offending field
// name) if any text-search operator targets a derived field. Non-text
// operators and non-derived fields are left alone so that callers can add the
// check without changing existing behaviour. A nil or empty derived map is a
// no-op.
func ValidateClauseAgainstDerivedFields(clause *WhereClause, derived map[string]bool) error {
	if clause == nil || len(derived) == 0 {
		return nil
	}

	if textSearchClauseTypes[clause.Type] {
		if clause.Field != "" && derived[clause.Field] {
			return fmt.Errorf("%w: field %q type %q", ErrDerivedPropertyTextSearchUnsupported, clause.Field, clause.Type)
		}
	}

	switch clause.Type {
	case "and", "or":
		var subs []WhereClause
		if err := json.Unmarshal(clause.Value, &subs); err != nil {
			return nil
		}
		for i := range subs {
			if err := ValidateClauseAgainstDerivedFields(&subs[i], derived); err != nil {
				return err
			}
		}
	case "not":
		var sub WhereClause
		if err := json.Unmarshal(clause.Value, &sub); err != nil {
			return nil
		}
		if err := ValidateClauseAgainstDerivedFields(&sub, derived); err != nil {
			return err
		}
	}

	return nil
}
