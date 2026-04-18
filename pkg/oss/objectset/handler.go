package objectset

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

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

// PropertyFilterProvider returns the set of property API names that the
// caller in ctx is permitted to see on objectType. The return convention
// mirrors security.Engine.AllowedProperties: a nil slice means "no
// PROPERTY-scope policy attached, allow all fields" and a non-nil slice
// (including zero-length) is an explicit allow list that downstream
// WireObject serialization must enforce by omitting unlisted fields. Kept
// as an interface so pkg/oss/objectset avoids a direct pkg/security import;
// cmd/server wires a thin adapter that forwards to *security.Engine.
type PropertyFilterProvider interface {
	AllowedProperties(ctx context.Context, objectType string) ([]string, error)
}

// DataAccessAuditor records successful loadObjectSet reads for the US-264
// per-ObjectType audit toggle. RecordLoadObjectSet is a best-effort sink —
// implementations decide whether the target ObjectType has opted in and
// silently drop rows for opted-out types. Kept as an interface so
// pkg/oss/objectset does not need to import pkg/audit or pkg/oms directly;
// cmd/server wires a thin adapter that forwards to oss.DataAccessAuditor.
type DataAccessAuditor interface {
	RecordLoadObjectSet(ctx context.Context, ontologyRID, objectTypeAPIName string, details map[string]any)
}

// Handler handles ObjectSet HTTP requests.
type Handler struct {
	executor           *Executor
	indexMgr           *index.Manager
	store              *Store
	aggEngine          *aggregation.Engine
	propertyFilter     PropertyFilterProvider
	historySnapshots   HistorySnapshotProvider
	persistedSnapshots PersistedSnapshotStore
	dataAccessAuditor  DataAccessAuditor
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

// SetPropertyFilterProvider wires the optional US-048 column-level
// visibility hook. When attached, every Load path (LoadObjects, LoadLinks,
// loadObjectSet) runs its result through the provider and strips any
// WireObject property not in the returned allow list before serialization.
// Passing nil detaches the hook. Safe to call at any point during server
// boot; the Handler re-reads the field on every request.
func (h *Handler) SetPropertyFilterProvider(p PropertyFilterProvider) {
	h.propertyFilter = p
}

// SetDataAccessAuditor wires the optional US-264 loadObjectSet audit sink.
// When attached and the target ObjectType has opted in via AuditDataAccess,
// every successful LoadObjects call emits an audit_events row (action =
// "data.access"). Passing nil detaches the hook.
func (h *Handler) SetDataAccessAuditor(a DataAccessAuditor) {
	h.dataAccessAuditor = a
}

// SetHistorySnapshotProvider wires the optional US-223 time-travel reader.
// When attached, LoadObjects honours the `?asOf=<RFC3339>` query parameter
// by routing through the provider instead of the live Bleve index. Passing
// nil detaches the hook (asOf requests then return 501). The reader is only
// consulted for "base" ObjectSet definitions; composite types (filter,
// union, intersect, ...) reject asOf with a 400 because Bleve has no
// per-instant snapshot to filter against.
func (h *Handler) SetHistorySnapshotProvider(p HistorySnapshotProvider) {
	h.historySnapshots = p
}

// applyPropertyVisibility is the Handler-side chokepoint that US-048
// column-level policies flow through. It resolves the allow list for the
// caller via the wired PropertyFilterProvider and filters every object in
// objs via WireObject.FilterProperties. A nil provider, nil allowed list,
// or empty input slice short-circuits to the input unchanged so existing
// back-compat tests don't pay the copy cost. Errors surface unchanged so
// callers can emit the proper apierror response.
func (h *Handler) applyPropertyVisibility(ctx context.Context, objectType string, objs []*oss.WireObject) ([]*oss.WireObject, error) {
	if h.propertyFilter == nil || len(objs) == 0 {
		return objs, nil
	}
	allowed, err := h.propertyFilter.AllowedProperties(ctx, objectType)
	if err != nil {
		return nil, err
	}
	if allowed == nil {
		return objs, nil
	}
	out := make([]*oss.WireObject, len(objs))
	for i, o := range objs {
		out[i] = o.FilterProperties(allowed)
	}
	return out, nil
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
	ontologyAPIName := chi.URLParam(r, "ontologyApiName")
	ctx := WithOntologyScope(r.Context(), ontologyAPIName)

	// US-223: ?asOf=<RFC3339> short-circuits to the time-travel path. We
	// scan object_history for the snapshot covering the requested instant
	// and skip the Bleve fetch entirely. Only "base" ObjectSets are
	// supported because composite types (filter / union / ...) need a
	// per-instant Bleve index that we don't materialise.
	if asOfRaw := r.URL.Query().Get("asOf"); asOfRaw != "" {
		asOf, err := time.Parse(time.RFC3339, asOfRaw)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAsOf", map[string]string{
				"asOf":   asOfRaw,
				"reason": "asOf must be an RFC3339 timestamp, e.g. 2026-01-01T00:00:00Z",
			}))
			return
		}
		h.loadObjectsAsOf(w, r, ctx, ontologyAPIName, &req, asOf)
		return
	}

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

		// US-210: surface per-edge properties produced by a searchAround
		// step. Values are injected under "__edge" to avoid colliding with
		// object properties. Absent when the traversal wasn't searchAround
		// or no edge carried properties.
		if edge, ok := result.EdgeProperties[pk]; ok && len(edge) > 0 {
			if props == nil {
				props = make(map[string]interface{}, 1)
			}
			props["__edge"] = edge
		}

		data = append(data, oss.FormatObject(result.ObjectType, pk, props))
	}

	// US-048: drop property fields the caller is not permitted to see. No-op
	// when no PROPERTY-scope policy is attached to result.ObjectType.
	data, err = h.applyPropertyVisibility(ctx, result.ObjectType, data)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PropertyFilterFailed", map[string]string{"error": err.Error()}))
		return
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

	if h.dataAccessAuditor != nil {
		h.dataAccessAuditor.RecordLoadObjectSet(ctx, ontologyAPIName, result.ObjectType, map[string]any{
			"count":      len(data),
			"totalCount": totalCount,
			"truncated":  result.Truncated,
		})
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// loadObjectsAsOf serves the US-223 time-travel branch of LoadObjects. It
// resolves the ObjectSet to a single base ObjectType, asks the wired
// HistorySnapshotProvider for every PK whose [valid_from, valid_to)
// interval covers asOf, then applies select / pagination exactly like the
// live path. Errors before any data is written so the response stays a
// regular JSON envelope.
func (h *Handler) loadObjectsAsOf(w http.ResponseWriter, r *http.Request, ctx context.Context, ontologyAPIName string, req *LoadObjectSetRequest, asOf time.Time) {
	if h.historySnapshots == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeTravelUnavailable", map[string]string{
			"reason": "history snapshot provider is not configured on this server",
		}))
		return
	}
	if req.ObjectSet.Type != "base" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeTravelUnsupportedObjectSet", map[string]string{
			"objectSetType": req.ObjectSet.Type,
			"reason":        "asOf time-travel currently only supports base ObjectSet definitions",
		}))
		return
	}
	if req.ObjectSet.ObjectType == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectType", map[string]string{
			"reason": "base ObjectSet requires objectType for asOf time-travel",
		}))
		return
	}

	snapshots, err := h.historySnapshots.SnapshotObjectsAt(ctx, ontologyAPIName, req.ObjectSet.ObjectType, asOf)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeTravelFailed", map[string]string{
			"asOf":  asOf.Format(time.RFC3339),
			"error": err.Error(),
		}))
		return
	}

	// Sort PKs ASC for stable pagination. The live path inherits Bleve's
	// internal order; the asOf path has no equivalent so deterministic-by-PK
	// is the safest default.
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].PrimaryKey < snapshots[j].PrimaryKey
	})

	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	offset := 0
	if req.PageToken != "" {
		if cursor, err := pagination.DecodeCursor(req.PageToken); err == nil {
			offset = cursor.Offset
		}
	}
	totalCount := len(snapshots)
	start := offset
	if start > totalCount {
		start = totalCount
	}
	end := start + pageSize
	if end > totalCount {
		end = totalCount
	}
	pageSnaps := snapshots[start:end]

	data := make([]*oss.WireObject, 0, len(pageSnaps))
	for _, snap := range pageSnaps {
		props := snap.Properties
		if len(req.Select) > 0 {
			filtered := make(map[string]interface{}, len(req.Select))
			for _, f := range req.Select {
				if v, ok := props[f]; ok {
					filtered[f] = v
				}
			}
			props = filtered
		}
		data = append(data, oss.FormatObject(req.ObjectSet.ObjectType, snap.PrimaryKey, props))
	}

	data, err = h.applyPropertyVisibility(ctx, req.ObjectSet.ObjectType, data)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PropertyFilterFailed", map[string]string{"error": err.Error()}))
		return
	}

	resp := &LoadObjectSetResponse{
		Data:               data,
		TotalCount:         strconv.Itoa(totalCount),
		TotalCountAccuracy: "EXACT",
	}
	if end < totalCount {
		nextCursor := &pagination.Cursor{Offset: end}
		resp.NextPageToken = nextCursor.Encode()
	}

	if h.dataAccessAuditor != nil {
		h.dataAccessAuditor.RecordLoadObjectSet(ctx, ontologyAPIName, req.ObjectSet.ObjectType, map[string]any{
			"count":      len(data),
			"totalCount": totalCount,
			"asOf":       asOf.Format(time.RFC3339),
		})
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// AggregateObjectSetRequest is the request for objectSet aggregation.
type AggregateObjectSetRequest struct {
	ObjectSet       *Definition                      `json:"objectSet"`
	Aggregation     []aggregation.AggregationSpec    `json:"aggregation"`
	GroupBy         []aggregation.GroupBySpec        `json:"groupBy,omitempty"`
	SubAggregations []aggregation.SubAggregationSpec `json:"subAggregations,omitempty"`
	Having          []aggregation.HavingClause       `json:"having,omitempty"`
	Cube            bool                             `json:"cube,omitempty"`
	Rollup          bool                             `json:"rollup,omitempty"`
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

	// When the ObjectSet produced withProperties-derived values AND at least
	// one metric targets a derived field, route through the in-memory path
	// that reads values straight from Result.DerivedValues. The Bleve-facet
	// engine would otherwise return nil for any derived metric because the
	// field is not present in the base index.
	if aggregationNeedsDerivedPath(req.Aggregation, result.DerivedValues) {
		aggResult, err := h.aggregateWithDerived(ctx, result, &req)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("AggregationFailed", map[string]string{"error": err.Error()}))
			return
		}
		httputil.WriteJSON(w, http.StatusOK, aggResult)
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
		ObjectType:      result.ObjectType,
		Aggregations:    req.Aggregation,
		GroupBy:         req.GroupBy,
		SubAggregations: req.SubAggregations,
		Having:          req.Having,
		Cube:            req.Cube,
		Rollup:          req.Rollup,
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
