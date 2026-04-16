package where

import (
	"context"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/searcher"
	index "github.com/blevesearch/bleve_index_api"
)

// PhrasePrefixQuery matches documents where the given terms appear in
// order (adjacent positions), with the last term treated as a prefix.
// This implements autocomplete/typeahead behavior similar to
// Elasticsearch's match_phrase_prefix query.
//
// For example, with terms ["john", "s"] and field "name":
//   - "John Smith" matches: "john" at pos 0, "smith" (prefix "s") at pos 1
//   - "Smith John" does NOT match: positional order is wrong
type PhrasePrefixQuery struct {
	Terms []string // lowercased terms; last is a prefix
	FieldVal string
	BoostVal float64
}

// NewPhrasePrefixQuery creates a phrase-prefix query. The terms slice
// must have at least 2 elements (single-term queries should use PrefixQuery).
func NewPhrasePrefixQuery(terms []string, field string) *PhrasePrefixQuery {
	return &PhrasePrefixQuery{
		Terms:    terms,
		FieldVal: field,
		BoostVal: 1.0,
	}
}

func (q *PhrasePrefixQuery) SetField(f string) { q.FieldVal = f }
func (q *PhrasePrefixQuery) Field() string     { return q.FieldVal }
func (q *PhrasePrefixQuery) SetBoost(b float64) { q.BoostVal = b }
func (q *PhrasePrefixQuery) Boost() float64     { return q.BoostVal }

// Searcher implements query.Query. It expands the last term prefix via
// the index dictionary and delegates to NewMultiPhraseSearcher so that
// positional adjacency is enforced across all terms.
func (q *PhrasePrefixQuery) Searcher(ctx context.Context, i index.IndexReader, m mapping.IndexMapping, options search.SearcherOptions) (search.Searcher, error) {
	lastPrefix := q.Terms[len(q.Terms)-1]

	// Expand prefix into all matching index terms.
	fieldDict, err := i.FieldDictPrefix(q.FieldVal, []byte(lastPrefix))
	if err != nil {
		return nil, err
	}
	defer fieldDict.Close()

	var expandedTerms []string
	for {
		entry, err := fieldDict.Next()
		if err != nil {
			return nil, err
		}
		if entry == nil {
			break
		}
		expandedTerms = append(expandedTerms, entry.Term)
	}

	if len(expandedTerms) == 0 {
		// No index terms match the prefix — return an empty result set.
		return searcher.NewMatchNoneSearcher(i)
	}

	// Build multi-phrase terms: exact match per position, alternatives
	// (expanded prefix terms) only at the last position.
	phraseTerms := make([][]string, len(q.Terms))
	for idx := 0; idx < len(q.Terms)-1; idx++ {
		phraseTerms[idx] = []string{q.Terms[idx]}
	}
	phraseTerms[len(q.Terms)-1] = expandedTerms

	return searcher.NewMultiPhraseSearcher(ctx, i, phraseTerms, 0, false, q.FieldVal, q.BoostVal, options)
}
