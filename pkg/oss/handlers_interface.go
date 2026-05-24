package oss

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/pagination"
	"github.com/liyang/weave/pkg/oss/where"
)

// InterfaceListObjects handles GET /api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceType}.
// Returns objects from ALL ObjectTypes that implement the specified interface.
func (h *Handler) InterfaceListObjects(w http.ResponseWriter, r *http.Request) {
	if h.omsRepo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("InterfaceQueryNotConfigured", nil))
		return
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	interfaceType := chi.URLParam(r, "interfaceType")

	_, objectTypes := h.resolveInterfaceOrWriteError(w, r, ontologyRID, interfaceType)
	if objectTypes == nil {
		return
	}

	pageSize := 0
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		var parseErr error
		pageSize, parseErr = strconv.Atoi(ps)
		if parseErr != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPageSize", map[string]string{
				"pageSize": ps,
			}))
			return
		}
	}

	pageToken := r.URL.Query().Get("pageToken")

	page, err := h.listObjectsAcrossTypes(ontologyRID, objectTypes, pageSize, pageToken)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("InterfaceListObjectsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, page)
}

// InterfaceSearchObjects handles POST /api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceType}/search.
// Searches across ALL ObjectTypes that implement the specified interface.
func (h *Handler) InterfaceSearchObjects(w http.ResponseWriter, r *http.Request) {
	if h.omsRepo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("InterfaceQueryNotConfigured", nil))
		return
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	interfaceType := chi.URLParam(r, "interfaceType")

	_, objectTypes := h.resolveInterfaceOrWriteError(w, r, ontologyRID, interfaceType)
	if objectTypes == nil {
		return
	}

	var body searchRequestBody
	if err := httputil.ReadJSON(r, &body); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	// Foundry V2: select is REQUIRED
	if len(body.Select) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SelectRequired", map[string]string{
			"reason": "SearchObjectsRequestV2.select is required and must be a non-empty array of property apiNames",
		}))
		return
	}

	page, err := h.searchObjectsAcrossTypes(ontologyRID, objectTypes, body)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("InterfaceSearchFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, page)
}

// InterfaceAggregateObjects handles POST /api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceType}/aggregate.
// Aggregates across ALL ObjectTypes that implement the specified interface.
func (h *Handler) InterfaceAggregateObjects(w http.ResponseWriter, r *http.Request) {
	if h.omsRepo == nil || h.aggEngine == nil || h.indexMgr == nil {
		apierror.WriteJSON(w, apierror.NewInternal("InterfaceAggregationNotConfigured", nil))
		return
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	interfaceType := chi.URLParam(r, "interfaceType")

	_, objectTypes := h.resolveInterfaceOrWriteError(w, r, ontologyRID, interfaceType)
	if objectTypes == nil {
		return
	}

	var req aggregation.AggregationRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAggregationRequest", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	ctx := index.WithOntologyScope(r.Context(), ontologyRID)
	for _, ot := range objectTypes {
		if apiErr := h.rejectFilteredAggregationFields(ctx, ot.APIName, &req); apiErr != nil {
			apierror.WriteJSON(w, apiErr)
			return
		}
	}

	if apiErr := rejectUnsupportedAggregationWhere(&req); apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

	result, err := h.aggregateAcrossTypes(ontologyRID, objectTypes, &req)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("InterfaceAggregationFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// InterfaceListLinkedObjects handles GET /api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceType}/{objectType}/{primaryKey}/links/{interfaceLinkType}.
// Lists linked objects through an interface link type for a specific object.
func (h *Handler) InterfaceListLinkedObjects(w http.ResponseWriter, r *http.Request) {
	if h.omsRepo == nil {
		apierror.WriteJSON(w, apierror.NewInternal("InterfaceQueryNotConfigured", nil))
		return
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	interfaceType := chi.URLParam(r, "interfaceType")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")
	interfaceLinkType := chi.URLParam(r, "interfaceLinkType")

	// Resolve interface — verify it exists
	_, err := h.omsRepo.GetInterfaceByAPIName(r.Context(), ontologyRID, interfaceType)
	if err != nil {
		if err == oms.ErrNotFound {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceNotFound", map[string]string{
				"interfaceType": interfaceType,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceFailed", nil))
		return
	}

	// Delegate to the regular linked objects service. The interface link type
	// apiName maps to a concrete LinkType on the implementing ObjectType.
	pageSize := 0
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		pageSize, _ = strconv.Atoi(ps)
	}

	page, err := h.svc.ListLinkedObjects(r.Context(), LinkedObjectsRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
		PrimaryKey:  primaryKey,
		LinkType:    interfaceLinkType,
		PageSize:    pageSize,
		PageToken:   r.URL.Query().Get("pageToken"),
	})
	if err != nil {
		if err == oms.ErrNotFound {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkedObjectNotFound", map[string]string{
				"objectType":        objectType,
				"primaryKey":        primaryKey,
				"interfaceLinkType": interfaceLinkType,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("InterfaceLinkedObjectsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, page)
}

// resolveInterfaceOrWriteError resolves an interface and writes the error
// response if resolution fails. Returns the interface and implementing
// ObjectTypes, or nil if an error was written.
func (h *Handler) resolveInterfaceOrWriteError(w http.ResponseWriter, r *http.Request, ontologyRID, interfaceType string) (*oms.Interface, []oms.ObjectType) {
	iface, err := h.omsRepo.GetInterfaceByAPIName(r.Context(), ontologyRID, interfaceType)
	if err != nil {
		if err == oms.ErrNotFound {
			apierror.WriteJSON(w, apierror.NewNotFound("InterfaceNotFound", map[string]string{
				"interfaceType": interfaceType,
			}))
		} else {
			apierror.WriteJSON(w, apierror.NewInternal("GetInterfaceFailed", nil))
		}
		return nil, nil
	}

	objectTypes, err := h.omsRepo.ListInterfaceObjectTypes(r.Context(), iface.RID)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("ListInterfaceObjectTypesFailed", nil))
		return nil, nil
	}

	return iface, objectTypes
}

// listObjectsAcrossTypes queries Bleve indexes for all implementing
// ObjectTypes and returns a merged, paginated ObjectPage.
func (h *Handler) listObjectsAcrossTypes(ontologyAPIName string, objectTypes []oms.ObjectType, pageSize int, pageToken string) (*ObjectPage, error) {
	if h.indexMgr == nil {
		return nil, fmt.Errorf("index manager not configured")
	}

	cursor, err := pagination.DecodeCursor(pageToken)
	if err != nil {
		return nil, err
	}

	if pageSize <= 0 {
		pageSize = pagination.DefaultPageSize
	}
	if pageSize > pagination.MaxPageSize {
		pageSize = pagination.MaxPageSize
	}

	// Collect all objects from all implementing types
	var allObjects []*WireObject
	for _, ot := range objectTypes {
		searchReq := bleve.NewSearchRequest(bleve.NewMatchAllQuery())
		searchReq.Fields = []string{"*"}
		searchReq.Size = pagination.MaxPageSize

		result, err := h.indexMgr.Search(scopedBleveKey(h.indexMgr, ontologyAPIName, ot.APIName), searchReq)
		if err != nil {
			continue // skip types with no index
		}

		for _, hit := range result.Hits {
			pk := ""
			if v, ok := hit.Fields[ot.PrimaryKey]; ok {
				pk = fmt.Sprintf("%v", v)
			}
			allObjects = append(allObjects, FormatObject(ot.APIName, pk, hit.Fields))
		}
	}

	totalCount := len(allObjects)

	// Apply pagination
	start := cursor.Offset
	if start > totalCount {
		start = totalCount
	}
	end := start + pageSize
	if end > totalCount {
		end = totalCount
	}

	page := &ObjectPage{
		Data:       allObjects[start:end],
		TotalCount: strconv.Itoa(totalCount),
	}

	nextOffset := cursor.Offset + pageSize
	if nextOffset < totalCount {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	return page, nil
}

// searchObjectsAcrossTypes searches Bleve indexes for all implementing
// ObjectTypes with a where clause and returns merged results.
func (h *Handler) searchObjectsAcrossTypes(ontologyAPIName string, objectTypes []oms.ObjectType, body searchRequestBody) (*ObjectPage, error) {
	if h.indexMgr == nil {
		return nil, fmt.Errorf("index manager not configured")
	}

	var bleveQuery query.Query
	if body.Where != nil {
		var err error
		bleveQuery, err = where.ConvertToBleveQuery(body.Where)
		if err != nil {
			return nil, err
		}
	} else {
		bleveQuery = bleve.NewMatchAllQuery()
	}

	cursor, err := pagination.DecodeCursor(body.PageToken)
	if err != nil {
		return nil, err
	}

	pageSize := body.PageSize
	if pageSize <= 0 {
		pageSize = pagination.DefaultPageSize
	}
	if pageSize > pagination.MaxPageSize {
		pageSize = pagination.MaxPageSize
	}

	// Search across all implementing types
	var allObjects []*WireObject
	for _, ot := range objectTypes {
		searchReq := bleve.NewSearchRequest(bleveQuery)
		searchReq.Fields = []string{"*"}
		searchReq.Size = pagination.MaxPageSize

		result, err := h.indexMgr.Search(scopedBleveKey(h.indexMgr, ontologyAPIName, ot.APIName), searchReq)
		if err != nil {
			continue // skip types with no index or no matches
		}

		for _, hit := range result.Hits {
			pk := ""
			if v, ok := hit.Fields[ot.PrimaryKey]; ok {
				pk = fmt.Sprintf("%v", v)
			}
			allObjects = append(allObjects, FormatObject(ot.APIName, pk, hit.Fields))
		}
	}

	totalCount := len(allObjects)

	// Apply pagination
	start := cursor.Offset
	if start > totalCount {
		start = totalCount
	}
	end := start + pageSize
	if end > totalCount {
		end = totalCount
	}

	page := &ObjectPage{
		Data:       allObjects[start:end],
		TotalCount: strconv.Itoa(totalCount),
	}

	nextOffset := cursor.Offset + pageSize
	if nextOffset < totalCount {
		nextCursor := &pagination.Cursor{Offset: nextOffset}
		page.NextPageToken = nextCursor.Encode()
	}

	return page, nil
}

// aggregateAcrossTypes aggregates across all implementing ObjectTypes.
// For each type with a Bleve index, runs the aggregation and merges results.
func (h *Handler) aggregateAcrossTypes(ontologyAPIName string, objectTypes []oms.ObjectType, req *aggregation.AggregationRequest) (*aggregation.AggregationResponse, error) {
	var merged *aggregation.AggregationResponse

	for _, ot := range objectTypes {
		idx := h.indexMgr.GetIndex(scopedBleveKey(h.indexMgr, ontologyAPIName, ot.APIName))
		if idx == nil {
			continue
		}

		perTypeReq := *req
		perTypeReq.ObjectType = ot.APIName

		result, err := h.aggEngine.Aggregate(idx, &perTypeReq)
		if err != nil {
			return nil, err
		}

		if merged == nil {
			merged = result
		} else {
			merged = mergeAggregationResponses(merged, result)
		}
	}

	if merged == nil {
		merged = &aggregation.AggregationResponse{}
	}

	return merged, nil
}

// mergeAggregationResponses merges two aggregation responses by summing
// their excluded items counts. Group-by results are concatenated.
// US-382: ComputeUsage is also accumulated — scannedRows + durationMs are
// summed across the per-type sub-aggregations, and Accuracy collapses to
// "APPROXIMATE" if either side surfaced an approximate result.
func mergeAggregationResponses(a, b *aggregation.AggregationResponse) *aggregation.AggregationResponse {
	result := &aggregation.AggregationResponse{
		ExcludedItems: a.ExcludedItems + b.ExcludedItems,
	}

	// Merge data arrays
	result.Data = append(a.Data, b.Data...)

	if a.ComputeUsage != nil || b.ComputeUsage != nil {
		merged := &aggregation.ComputeUsage{}
		mergedAccuracy := "ACCURATE"
		if a.ComputeUsage != nil {
			merged.ScannedRows += a.ComputeUsage.ScannedRows
			merged.DurationMs += a.ComputeUsage.DurationMs
			if a.ComputeUsage.Accuracy == "APPROXIMATE" {
				mergedAccuracy = "APPROXIMATE"
			}
		}
		if b.ComputeUsage != nil {
			merged.ScannedRows += b.ComputeUsage.ScannedRows
			merged.DurationMs += b.ComputeUsage.DurationMs
			if b.ComputeUsage.Accuracy == "APPROXIMATE" {
				mergedAccuracy = "APPROXIMATE"
			}
		}
		merged.Accuracy = mergedAccuracy
		result.ComputeUsage = merged
	}
	if a.Accuracy == "APPROXIMATE" || b.Accuracy == "APPROXIMATE" {
		result.Accuracy = "APPROXIMATE"
	} else if a.Accuracy == "ACCURATE" || b.Accuracy == "ACCURATE" {
		result.Accuracy = "ACCURATE"
	}

	return result
}
