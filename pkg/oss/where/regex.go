package where

import (
	"encoding/json"
	"fmt"
	"regexp/syntax"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// Regex search safety bounds.
//
//   - MaxRegexPatternLength caps the raw pattern string length. Bleve compiles
//     patterns through `vellum`/`regexp/syntax` (RE2-style FSA) so catastrophic
//     backtracking is structurally impossible, but very long patterns can still
//     blow up the FSA state count and slow things down. 1024 chars is the
//     practical ceiling — well above any sensible end-user regex and well
//     below the point where FSA construction starts to hurt.
//
//   - RegexQueryTimeout is the per-search wall-clock cap enforced at the OSS
//     service layer when the where tree contains a regex clause. The timeout
//     is propagated through `bleve.Index.SearchInContext`, so even a malicious
//     pattern that compiles cheaply but iterates over a huge term dictionary
//     will be cancelled at 500ms. Pure-RE2 engines don't backtrack, but the
//     PRD asks for an explicit ceiling and this is where it lives.
const (
	MaxRegexPatternLength = 1024
	RegexQueryTimeout     = 500 * time.Millisecond
)

// convertRegex handles the "regex" operator using a Bleve RegexpQuery.
//
// The pattern is validated up-front via regexp/syntax (RE2 dialect — same
// engine bleve uses) so malformed input returns a clean error at convert time
// rather than a confusing search-time failure.
//
// Pre-lowercasing matches the analyser-driven lowercasing applied at index
// time (consistent with the fuzzy / startsWith / wildcard operators); a
// pattern like `^A.*` becomes `^a.*` and matches the indexed term `apple`.
// Callers who need case-sensitive regex matching should map the field with a
// keyword analyser at object-type definition time.
//
// Bleve's vellum-backed regex engine anchors patterns to the FULL indexed
// term, so `a.*` already matches "alice"; an explicit `^` start anchor is
// tolerated (no-op) but `$` and other zero-width assertions are rejected by
// vellum at search time and surfaced as a 400 by the OSS handler. We don't
// strip the anchors here — it lets users write familiar regex syntax and the
// search-time error is precise enough to act on.
func convertRegex(clause *WhereClause) (query.Query, error) {
	var strVal string
	if err := json.Unmarshal(clause.Value, &strVal); err != nil {
		return nil, fmt.Errorf("regex value must be a string: %w", err)
	}
	if strings.TrimSpace(strVal) == "" {
		return nil, fmt.Errorf("regex value must be a non-empty string")
	}
	if len(strVal) > MaxRegexPatternLength {
		return nil, fmt.Errorf("regex pattern length %d exceeds max %d", len(strVal), MaxRegexPatternLength)
	}
	if _, err := syntax.Parse(strVal, syntax.Perl); err != nil {
		return nil, fmt.Errorf("regex pattern invalid: %w", err)
	}

	q := bleve.NewRegexpQuery(strings.ToLower(strVal))
	q.SetField(clause.Field)
	return q, nil
}

// HasRegexClause reports whether the given where tree contains at least one
// `regex` operator. Used by the OSS service layer to decide whether to wrap
// the bleve search in a RegexQueryTimeout-scoped context.
func HasRegexClause(clause *WhereClause) bool {
	if clause == nil {
		return false
	}
	switch clause.Type {
	case "regex":
		return true
	case "and", "or":
		var subs []WhereClause
		if err := json.Unmarshal(clause.Value, &subs); err != nil {
			return false
		}
		for i := range subs {
			if HasRegexClause(&subs[i]) {
				return true
			}
		}
	case "not":
		var subs []WhereClause
		if err := json.Unmarshal(clause.Value, &subs); err == nil {
			for i := range subs {
				if HasRegexClause(&subs[i]) {
					return true
				}
			}
			return false
		}
		var single WhereClause
		if err := json.Unmarshal(clause.Value, &single); err == nil {
			return HasRegexClause(&single)
		}
	}
	return false
}
