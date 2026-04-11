package objectset

import (
	"net/http"
	"strconv"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/pagination"
)

// LoadObjectSetRequest is the Palantir V2 request format for loadObjects.
type LoadObjectSetRequest struct {
	ObjectSet *Definition `json:"objectSet"`
	Select    []string    `json:"select,omitempty"`
	OrderBy   *OrderBy    `json:"orderBy,omitempty"`
	PageSize  int         `json:"pageSize,omitempty"`
	PageToken string      `json:"pageToken,omitempty"`
	Snapshot  bool        `json:"snapshot,omitempty"`
}

// OrderBy specifies ordering for loaded objects.
type OrderBy struct {
	Fields []OrderByField `json:"fields"`
}

// OrderByField specifies a single field ordering.
type OrderByField struct {
	Field     string `json:"field"`
	Direction string `json:"direction"` // "asc" or "desc"
}

// LoadObjectSetResponse is the Palantir V2 response format with string totalCount.
type LoadObjectSetResponse struct {
	Data          []*oss.WireObject `json:"data"`
	NextPageToken string            `json:"nextPageToken,omitempty"`
	TotalCount    string            `json:"totalCount,omitempty"`
	// TotalCountAccuracy is "EXACT" when the ObjectSet was fully materialized
	// and "APPROXIMATE" when the executor hit its hard cap (10000 PKs) and
	// truncated the result. Callers should warn the user that totalCount and
	// data are partial when this is "APPROXIMATE".
	TotalCountAccuracy string `json:"totalCountAccuracy,omitempty"`
}

// CreateTemporaryRequest is the request for creating a temporary ObjectSet.
type CreateTemporaryRequest struct {
	ObjectSet *Definition `json:"objectSet"`
}

// CreateTemporaryResponse is the response for creating a temporary ObjectSet.
type CreateTemporaryResponse struct {
	ObjectSetRID string `json:"objectSetRid"`
}

// Handler handles ObjectSet HTTP requests.
type Handler struct {
	executor  *Executor
	indexMgr  *index.Manager
	store     *Store
	aggEngine *aggregation.Engine
}

// NewHandler creates a new ObjectSet handler.
func NewHandler(executor *Executor, indexMgr *index.Manager, store *Store) *Handler {
	return &Handler{
		executor:  executor,
		indexMgr:  indexMgr,
		store:     store,
		aggEngine: aggregation.NewEngine(),
	}
}

// LoadObjects handles POST /api/v2/ontologies/{ont}/objectSets/loadObjects.
func (h *Handler) LoadObjects(w http.ResponseWriter, r *http.Request) {
	var req LoadObjectSetRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ObjectSet == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectSet", nil))
		return
	}

	// Foundry V2: select is REQUIRED
	if len(req.Select) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SelectRequired", map[string]string{
			"reason": "LoadObjectSetRequestV2.select is required and must be a non-empty array of property apiNames",
		}))
		return
	}

	// Stamp the ontology scope on the context so the executor and downstream
	// Bleve lookups use per-ontology index keys (US-044).
	ctx := WithOntologyScope(r.Context(), chi.URLParam(r, "ontologyApiName"))

	// Execute the ObjectSet to get PKs
	result, err := h.executor.Execute(ctx, req.ObjectSet)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ObjectSetFailed", map[string]string{"error": err.Error()}))
		return
	}

	// Apply pagination
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	offset := 0
	if req.PageToken != "" {
		cursor, err := pagination.DecodeCursor(req.PageToken)
		if err == nil {
			offset = cursor.Offset
		}
	}

	totalCount := len(result.PrimaryKeys)

	// Slice for current page
	start := offset
	if start > totalCount {
		start = totalCount
	}
	end := start + pageSize
	if end > totalCount {
		end = totalCount
	}
	pagePKs := result.PrimaryKeys[start:end]

	// Load full objects from Bleve
	data := make([]*oss.WireObject, 0, len(pagePKs))

	// Determine which fields to request
	fields := []string{"*"}
	if len(req.Select) > 0 {
		fields = req.Select
	}

	for _, pk := range pagePKs {
		searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{pk}))
		searchReq.Fields = fields
		searchReq.Size = 1

		res, err := h.indexMgr.Search(scopedIndexKey(ctx, h.indexMgr, result.ObjectType), searchReq)
		if err != nil || len(res.Hits) == 0 {
			continue
		}

		props := res.Hits[0].Fields
		if len(req.Select) > 0 {
			filtered := make(map[string]interface{})
			for _, f := range req.Select {
				if v, ok := props[f]; ok {
					filtered[f] = v
				}
			}
			props = filtered
		}

		if derived, ok := result.DerivedValues[pk]; ok {
			if props == nil {
				props = make(map[string]interface{}, len(derived))
			}
			for k, v := range derived {
				props[k] = v
			}
		}

		data = append(data, oss.FormatObject(result.ObjectType, pk, props))
	}

	accuracy := "EXACT"
	if result.Truncated {
		accuracy = "APPROXIMATE"
	}
	resp := &LoadObjectSetResponse{
		Data:               data,
		TotalCount:         strconv.Itoa(totalCount),
		TotalCountAccuracy: accuracy,
	}

	// Set next page token
	if end < totalCount {
		nextCursor := &pagination.Cursor{Offset: end}
		resp.NextPageToken = nextCursor.Encode()
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// AggregateObjectSetRequest is the request for objectSet aggregation.
type AggregateObjectSetRequest struct {
	ObjectSet   *Definition                   `json:"objectSet"`
	Aggregation []aggregation.AggregationSpec `json:"aggregation"`
	GroupBy     []aggregation.GroupBySpec     `json:"groupBy,omitempty"`
}

// Aggregate handles POST /api/v2/ontologies/{ont}/objectSets/aggregate.
func (h *Handler) Aggregate(w http.ResponseWriter, r *http.Request) {
	var req AggregateObjectSetRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ObjectSet == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectSet", nil))
		return
	}

	ctx := WithOntologyScope(r.Context(), chi.URLParam(r, "ontologyApiName"))

	// Execute the ObjectSet to determine the object type and PKs.
	result, err := h.executor.Execute(ctx, req.ObjectSet)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ObjectSetFailed", map[string]string{"error": err.Error()}))
		return
	}

	idx := h.indexMgr.GetIndex(scopedIndexKey(ctx, h.indexMgr, result.ObjectType))
	if idx == nil {
		apierror.WriteJSON(w, apierror.NewNotFound("IndexNotFound", map[string]string{"objectType": result.ObjectType}))
		return
	}

	// Build a base query scoped to the ObjectSet's primary keys.
	var baseQuery query.Query
	if len(result.PrimaryKeys) > 0 {
		baseQuery = bleve.NewDocIDQuery(result.PrimaryKeys)
	} else {
		baseQuery = bleve.NewMatchAllQuery()
	}

	aggReq := &aggregation.AggregationRequest{
		ObjectType:   result.ObjectType,
		Aggregations: req.Aggregation,
		GroupBy:      req.GroupBy,
	}

	aggResult, err := h.aggEngine.AggregateWithQuery(idx, baseQuery, aggReq)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("AggregationFailed", map[string]string{"error": err.Error()}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, aggResult)
}

// CreateTemporary handles POST /api/v2/ontologies/{ont}/objectSets/createTemporary.
func (h *Handler) CreateTemporary(w http.ResponseWriter, r *http.Request) {
	var req CreateTemporaryRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ObjectSet == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectSet", nil))
		return
	}

	if err := req.ObjectSet.Validate(); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidObjectSet", map[string]string{"error": err.Error()}))
		return
	}

	id := h.store.Put(req.ObjectSet)
	httputil.WriteJSON(w, http.StatusOK, &CreateTemporaryResponse{
		ObjectSetRID: id,
	})
}
