package where

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	"github.com/blevesearch/bleve/v2/search/searcher"
	index "github.com/blevesearch/bleve_index_api"
)

// MaxPhraseSlop caps the configurable phrase slop distance. Lucene allows
// arbitrary slop but very large values trigger O(N·locations!) blowups during
// the path search; 32 is more than sufficient for PRD-scale phrases and
// matches the common upper bound seen in other engines.
const MaxPhraseSlop = 32

// PhraseSlopQuery matches documents where the given terms all appear and the
// sum of position gaps between consecutive terms (relative to strict adjacency)
// is at most Slop. slop=0 is equivalent to bleve's plain PhraseQuery (terms
// must be adjacent in order); higher slop tolerates gaps / reordering.
type PhraseSlopQuery struct {
	Terms    []string
	Slop     int
	FieldVal string
	BoostVal float64

	// AnalyzeTerms runs each term through the field's analyzer at search
	// time so the query compares against the INDEXED token form (e.g. the
	// "en" analyzer stems "migration" → "migrat"). The legacy "phrase"
	// operator keeps this off for backward compatibility (pre-lowercased
	// raw terms); the Foundry "interval" operator turns it on because its
	// contract is explicitly "the analyzed form of text fields".
	AnalyzeTerms bool
}

// NewPhraseSlopQuery builds a phrase-with-slop query over pre-lowercased terms.
// Callers are expected to lowercase input to match the default text analyser;
// at least one term is required.
func NewPhraseSlopQuery(terms []string, slop int, field string) *PhraseSlopQuery {
	return &PhraseSlopQuery{
		Terms:    terms,
		Slop:     slop,
		FieldVal: field,
		BoostVal: 1.0,
	}
}

func (q *PhraseSlopQuery) SetField(f string)    { q.FieldVal = f }
func (q *PhraseSlopQuery) Field() string        { return q.FieldVal }
func (q *PhraseSlopQuery) SetBoost(b float64)   { q.BoostVal = b }
func (q *PhraseSlopQuery) Boost() float64       { return q.BoostVal }

func (q *PhraseSlopQuery) Searcher(ctx context.Context, i index.IndexReader, m mapping.IndexMapping, options search.SearcherOptions) (search.Searcher, error) {
	if len(q.Terms) == 0 {
		return searcher.NewMatchNoneSearcher(i)
	}
	if q.Slop < 0 {
		return nil, fmt.Errorf("phrase slop must be non-negative, got %d", q.Slop)
	}

	// Position gaps are only surfaced when term vectors are included.
	opts := options
	opts.IncludeTermVectors = true

	terms := q.Terms
	if q.AnalyzeTerms {
		terms = analyzePhraseTerms(m, q.FieldVal, terms)
		if len(terms) == 0 {
			// Every term analyzed away (e.g. all stopwords): nothing can
			// positionally match.
			return searcher.NewMatchNoneSearcher(i)
		}
	}

	termSearchers := make([]search.Searcher, 0, len(terms))
	for _, term := range terms {
		if term == "" {
			for _, ts := range termSearchers {
				_ = ts.Close()
			}
			return nil, fmt.Errorf("phrase slop terms must be non-empty")
		}
		ts, err := searcher.NewTermSearcher(ctx, i, term, q.FieldVal, q.BoostVal, opts)
		if err != nil {
			for _, prior := range termSearchers {
				_ = prior.Close()
			}
			return nil, fmt.Errorf("phrase slop term searcher: %w", err)
		}
		termSearchers = append(termSearchers, ts)
	}

	if len(termSearchers) == 1 {
		return termSearchers[0], nil
	}

	conj, err := searcher.NewConjunctionSearcher(ctx, i, termSearchers, opts)
	if err != nil {
		for _, ts := range termSearchers {
			_ = ts.Close()
		}
		return nil, fmt.Errorf("phrase slop conjunction searcher: %w", err)
	}

	return &phraseSlopSearcher{
		inner: conj,
		terms: append([]string(nil), terms...),
		slop:  q.Slop,
		field: q.FieldVal,
	}, nil
}

// analyzePhraseTerms maps raw query terms onto their indexed token forms
// using the analyzer the mapping assigns to the field. Terms the analyzer
// drops entirely (stopwords) are skipped; a term that analyzes into several
// tokens contributes each of them in order. A nil analyzer (unknown name)
// falls back to the raw terms so the query degrades to legacy behavior
// instead of erroring.
func analyzePhraseTerms(m mapping.IndexMapping, field string, terms []string) []string {
	analyzer := m.AnalyzerNamed(m.AnalyzerNameForPath(field))
	if analyzer == nil {
		return terms
	}
	analyzed := make([]string, 0, len(terms))
	for _, term := range terms {
		for _, token := range analyzer.Analyze([]byte(term)) {
			analyzed = append(analyzed, string(token.Term))
		}
	}
	return analyzed
}

// phraseSlopSearcher wraps a conjunction of single-term searchers and drops
// documents whose term positions cannot form a phrase within the slop budget.
type phraseSlopSearcher struct {
	inner search.Searcher
	terms []string
	slop  int
	field string

	scratchLocations []search.Location
}

func (s *phraseSlopSearcher) Next(ctx *search.SearchContext) (*search.DocumentMatch, error) {
	for {
		dm, err := s.inner.Next(ctx)
		if err != nil || dm == nil {
			return dm, err
		}
		if s.matchesSlop(dm) {
			return dm, nil
		}
		ctx.DocumentMatchPool.Put(dm)
	}
}

func (s *phraseSlopSearcher) Advance(ctx *search.SearchContext, ID index.IndexInternalID) (*search.DocumentMatch, error) {
	dm, err := s.inner.Advance(ctx, ID)
	if err != nil || dm == nil {
		return dm, err
	}
	if s.matchesSlop(dm) {
		return dm, nil
	}
	ctx.DocumentMatchPool.Put(dm)
	return s.Next(ctx)
}

func (s *phraseSlopSearcher) Close() error                { return s.inner.Close() }
func (s *phraseSlopSearcher) Weight() float64             { return s.inner.Weight() }
func (s *phraseSlopSearcher) SetQueryNorm(n float64)      { s.inner.SetQueryNorm(n) }
func (s *phraseSlopSearcher) Count() uint64               { return s.inner.Count() }
func (s *phraseSlopSearcher) Min() int                    { return s.inner.Min() }
func (s *phraseSlopSearcher) Size() int                   { return s.inner.Size() }
func (s *phraseSlopSearcher) DocumentMatchPoolSize() int  { return s.inner.DocumentMatchPoolSize() }

func (s *phraseSlopSearcher) matchesSlop(dm *search.DocumentMatch) bool {
	// Complete() folds FieldTermLocations into Locations; must run before reading.
	s.scratchLocations = dm.Complete(s.scratchLocations)

	tlm, ok := dm.Locations[s.field]
	if !ok || len(tlm) == 0 {
		return false
	}
	return findSlopPath(s.terms, tlm, s.slop)
}

// findSlopPath returns true iff there exists an ordered sequence of term
// positions (one per phrase term, reused-position-free) whose gaps sum to
// at most `slop`. The algorithm mirrors bleve's findPhrasePaths but accepts
// a configurable slop budget (bleve hard-codes 0 at the caller level).
func findSlopPath(terms []string, tlm search.TermLocationMap, slop int) bool {
	if len(terms) == 0 {
		return true
	}
	return phraseSlopWalk(0, terms, tlm, slop, nil)
}

func phraseSlopWalk(prevPos uint64, terms []string, tlm search.TermLocationMap, remaining int, used []*search.Location) bool {
	if len(terms) == 0 {
		return true
	}
	term := terms[0]
	rest := terms[1:]
	locs := tlm[term]
	for _, loc := range locs {
		// disallow reusing a term+location already claimed by an earlier term
		reused := false
		for _, u := range used {
			if u == loc {
				reused = true
				break
			}
		}
		if reused {
			continue
		}
		dist := 0
		if prevPos != 0 {
			expected := prevPos + 1
			if expected > loc.Pos {
				dist = int(expected - loc.Pos)
			} else {
				dist = int(loc.Pos - expected)
			}
		}
		if prevPos != 0 && dist > remaining {
			continue
		}
		if phraseSlopWalk(loc.Pos, rest, tlm, remaining-dist, append(used, loc)) {
			return true
		}
	}
	return false
}
