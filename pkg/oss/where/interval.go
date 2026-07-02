package where

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// maxIntervalRuleDepth bounds the recursion of allOf/anyOf rule nesting so
// a pathological payload cannot blow the stack. Real interval queries are
// one or two levels deep; 16 is generous.
const maxIntervalRuleDepth = 16

// intervalRule is the decoded IntervalQueryRule union. One struct covers
// every variant because the Foundry members share disjoint field sets:
//
//	match             {query, ordered, maxGaps?}
//	allOf             {rules, ordered, maxGaps?}
//	anyOf             {rules}
//	prefixOnLastToken {query}
//	fuzzy             {term, fuzziness? (0-2, default 2)}
type intervalRule struct {
	Type      string            `json:"type"`
	Query     string            `json:"query"`
	Term      string            `json:"term"`
	Fuzziness *int              `json:"fuzziness"`
	MaxGaps   *int              `json:"maxGaps"`
	Ordered   bool              `json:"ordered"`
	Rules     []json.RawMessage `json:"rules"`
}

// convertInterval handles the Foundry SearchJsonQueryV2 "interval"
// operator (IntervalQuery): {"type":"interval","field":"<textProp>",
// "rule":{...}} evaluated against the ANALYZED form of text fields.
//
// Bleve has no Lucene intervals engine, so Weave maps each rule onto the
// closest native query with these documented approximations:
//
//   - match maxGaps maps onto the phrase-slop budget (slop counts the sum
//     of positional gaps, and — like Lucene slop — tolerates reordering
//     within the budget even when ordered=true).
//   - match ordered=true without maxGaps uses the MaxPhraseSlop budget
//     instead of unlimited gaps.
//   - allOf intersects its sub-rules; ordered/maxGaps ACROSS sub-rules
//     are not enforced.
//
// Term-membership semantics (which documents match at all) are exact for
// match-unordered, anyOf, prefixOnLastToken and fuzzy.
func convertInterval(clause *WhereClause) (query.Query, error) {
	if strings.TrimSpace(clause.Field) == "" {
		return nil, fmt.Errorf("interval requires a field")
	}
	if len(clause.Rule) == 0 || bytes.Equal(bytes.TrimSpace(clause.Rule), []byte("null")) {
		return nil, fmt.Errorf("interval requires a rule (match/allOf/anyOf/prefixOnLastToken/fuzzy)")
	}
	return convertIntervalRule(clause.Field, clause.Rule, 0)
}

func convertIntervalRule(field string, raw json.RawMessage, depth int) (query.Query, error) {
	if depth > maxIntervalRuleDepth {
		return nil, fmt.Errorf("interval rule nesting exceeds %d levels", maxIntervalRuleDepth)
	}
	var rule intervalRule
	if err := json.Unmarshal(raw, &rule); err != nil {
		return nil, fmt.Errorf("interval rule must be an object: %w", err)
	}

	switch rule.Type {
	case "match":
		return convertIntervalMatch(field, &rule)
	case "allOf":
		return convertIntervalComposite(field, &rule, depth, true)
	case "anyOf":
		return convertIntervalComposite(field, &rule, depth, false)
	case "prefixOnLastToken":
		return convertIntervalPrefixOnLastToken(field, &rule)
	case "fuzzy":
		return convertIntervalFuzzy(field, &rule)
	default:
		return nil, fmt.Errorf("unsupported interval rule type: %q", rule.Type)
	}
}

// convertIntervalMatch maps the "match" rule onto term conjunctions /
// phrase-slop queries depending on ordered + maxGaps.
func convertIntervalMatch(field string, rule *intervalRule) (query.Query, error) {
	if rule.MaxGaps != nil && (*rule.MaxGaps < 0 || *rule.MaxGaps > MaxPhraseSlop) {
		return nil, fmt.Errorf("interval match maxGaps must be in [0, %d], got %d", MaxPhraseSlop, *rule.MaxGaps)
	}

	terms := SplitTerms(rule.Query)
	if len(terms) == 0 {
		return bleve.NewMatchNoneQuery(), nil
	}
	if len(terms) == 1 {
		mq := bleve.NewMatchQuery(terms[0])
		mq.SetField(field)
		return mq, nil
	}

	// Unordered with no gap budget: every term must appear somewhere in
	// the field — a pure conjunction, position-free.
	if rule.MaxGaps == nil && !rule.Ordered {
		bq := bleve.NewBooleanQuery()
		for _, term := range terms {
			mq := bleve.NewMatchQuery(term)
			mq.SetField(field)
			bq.AddMust(mq)
		}
		return bq, nil
	}

	// Positional variants ride the phrase-slop searcher. maxGaps is the
	// slop budget; ordered without maxGaps gets the maximum budget.
	slop := MaxPhraseSlop
	if rule.MaxGaps != nil {
		slop = *rule.MaxGaps
	}
	lowered := make([]string, len(terms))
	for i, t := range terms {
		lowered[i] = strings.ToLower(t)
	}
	psq := NewPhraseSlopQuery(lowered, slop, field)
	// IntervalQuery is defined over the ANALYZED form of text fields, so
	// terms must be re-analyzed at search time to hit stemmed indexes.
	psq.AnalyzeTerms = true
	return psq, nil
}

// convertIntervalComposite handles allOf (conjunction) and anyOf
// (disjunction) over recursively converted sub-rules.
func convertIntervalComposite(field string, rule *intervalRule, depth int, conjunction bool) (query.Query, error) {
	kind := "anyOf"
	if conjunction {
		kind = "allOf"
	}
	if len(rule.Rules) == 0 {
		return nil, fmt.Errorf("interval %s requires a non-empty rules array", kind)
	}

	subs := make([]query.Query, 0, len(rule.Rules))
	for i, subRaw := range rule.Rules {
		sub, err := convertIntervalRule(field, subRaw, depth+1)
		if err != nil {
			return nil, fmt.Errorf("interval %s rules[%d]: %w", kind, i, err)
		}
		subs = append(subs, sub)
	}

	if conjunction {
		bq := bleve.NewBooleanQuery()
		for _, sub := range subs {
			bq.AddMust(sub)
		}
		return bq, nil
	}
	dq := bleve.NewDisjunctionQuery(subs...)
	dq.SetMin(1)
	return dq, nil
}

// convertIntervalPrefixOnLastToken reuses the autocomplete machinery: all
// terms in order, exact for all but the last, prefix on the last.
func convertIntervalPrefixOnLastToken(field string, rule *intervalRule) (query.Query, error) {
	terms := SplitTerms(rule.Query)
	if len(terms) == 0 {
		return bleve.NewMatchNoneQuery(), nil
	}

	lowered := make([]string, len(terms))
	for i, t := range terms {
		lowered[i] = strings.ToLower(t)
	}
	if len(lowered) == 1 {
		pq := bleve.NewPrefixQuery(lowered[0])
		pq.SetField(field)
		return pq, nil
	}
	return NewPhrasePrefixQuery(lowered, field), nil
}

// convertIntervalFuzzy maps the "fuzzy" rule onto a Bleve FuzzyQuery.
// Foundry defaults fuzziness to 2 (the Levenshtein maximum).
func convertIntervalFuzzy(field string, rule *intervalRule) (query.Query, error) {
	if strings.TrimSpace(rule.Term) == "" {
		return nil, fmt.Errorf("interval fuzzy requires a non-empty term")
	}
	fuzziness := 2
	if rule.Fuzziness != nil {
		fuzziness = *rule.Fuzziness
	}
	if fuzziness < 0 || fuzziness > MaxFuzziness {
		return nil, fmt.Errorf("interval fuzzy fuzziness must be in [0, %d], got %d", MaxFuzziness, fuzziness)
	}

	fq := bleve.NewFuzzyQuery(strings.ToLower(rule.Term))
	fq.SetField(field)
	fq.SetFuzziness(fuzziness)
	return fq, nil
}
