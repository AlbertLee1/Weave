package objectset

import (
	"context"
	"strconv"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/apierror"
)

// orderByMaxKeys caps how many primary keys the loadObjects orderBy path is
// willing to sort in a single request. Sorting happens over the FULL
// resolved PK set (it must — ordering after the pagination slice would make
// every page locally sorted but globally shuffled), so the cost scales with
// the ObjectSet size, not the page size. Base/filter executions are already
// bounded by BaseExecutionCap, but union and static definitions can exceed
// it; those requests get an explicit 422 instead of a silent unsorted
// fallback.
const orderByMaxKeys = BaseExecutionCap

// orderByTooLarge is the typed 422 for orderBy requests over ObjectSets
// larger than orderByMaxKeys.
func orderByTooLarge(count int) *apierror.APIError {
	return apierror.NewQueryTooLarge("OrderByTooLarge", map[string]string{
		"reason": "orderBy requires materializing and sorting the full ObjectSet before pagination; this ObjectSet exceeds the sortable cap — narrow it with a filter",
		"count":  strconv.Itoa(count),
		"cap":    strconv.Itoa(orderByMaxKeys),
	})
}

// sortPrimaryKeys reorders pks according to the pre-validated Bleve sort
// order (["-rank","name"], as produced by oss.OrderBy.BleveSortOrder). The
// sort is pushed down to Bleve via a DocID query over the full PK set so
// numeric / keyword fields compare by their indexed representation rather
// than by string-formatted property values. A trailing "_id" tiebreaker
// guarantees a total order, so ties can never flip between the page-1 and
// page-2 requests of the same query. PKs the index does not know (e.g.
// static definitions listing stale ids) keep their relative order at the
// tail so totalCount semantics stay intact.
func (h *Handler) sortPrimaryKeys(ctx context.Context, objectType string, pks []string, sortOrder []string) ([]string, error) {
	searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery(pks))
	searchReq.Size = len(pks)
	searchReq.SortBy(append(append(make([]string, 0, len(sortOrder)+1), sortOrder...), "_id"))

	res, err := h.indexMgr.Search(scopedIndexKey(ctx, h.indexMgr, objectType), searchReq)
	if err != nil {
		return nil, err
	}

	sorted := make([]string, 0, len(pks))
	seen := make(map[string]struct{}, len(res.Hits))
	for _, hit := range res.Hits {
		sorted = append(sorted, hit.ID)
		seen[hit.ID] = struct{}{}
	}
	for _, pk := range pks {
		if _, ok := seen[pk]; !ok {
			sorted = append(sorted, pk)
		}
	}
	return sorted, nil
}
