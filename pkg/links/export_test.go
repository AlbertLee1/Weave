package links

import (
	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/oms"
)

// Searcher is the test-only re-export of the package-private searcher
// interface so that perf tests can implement a counting decorator.
type Searcher interface {
	Search(objectType string, req *bleve.SearchRequest) (*bleve.SearchResult, error)
}

// NewResolverWithSearcher constructs a Resolver wired to an arbitrary
// Searcher. Used by perf tests to inject a counting wrapper around the real
// *index.Manager so the test can assert query counts (rather than wall time)
// when verifying that batch hydration is in effect.
func NewResolverWithSearcher(repo oms.Repository, s Searcher) *Resolver {
	return newResolverWithSearcher(repo, s)
}
