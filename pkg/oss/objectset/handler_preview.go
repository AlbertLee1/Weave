package objectset

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/blevesearch/bleve/v2"
	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oss/pagination"
)

// LoadObjectSetV2MultipleObjectTypesResponse is the preview response shape used
// by the Foundry loadObjectsMultipleObjectTypes and loadObjectsOrInterfaces
// endpoints. Each data item is a flat map that uses the `$rid` / `$primaryKey`
// / `$apiName` prefix (rather than the `__rid` prefix used by the stable V2
// wire format).
type LoadObjectSetV2MultipleObjectTypesResponse struct {
	Data               []map[string]interface{} `json:"data"`
	NextPageToken      string                   `json:"nextPageToken,omitempty"`
	TotalCount         string                   `json:"totalCount,omitempty"`
	TotalCountAccuracy string                   `json:"totalCountAccuracy,omitempty"`
}

// LoadObjectsMultipleObjectTypes handles
// POST /api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsMultipleObjectTypes.
//
// Foundry preview endpoint: requires ?preview=true. Result items use the
// `$rid` / `$primaryKey` / `$apiName` wire prefix so that callers can
// distinguish multi-type result rows.
func (h *Handler) LoadObjectsMultipleObjectTypes(w http.ResponseWriter, r *http.Request) {
	if !requirePreviewObjectSet(w, r) {
		return
	}
	h.loadPreview(w, r)
}

// LoadObjectsOrInterfaces handles
// POST /api/v2/ontologies/{ontologyApiName}/objectSets/loadObjectsOrInterfaces.
//
// Foundry preview endpoint: requires ?preview=true. Like
// loadObjectsMultipleObjectTypes but semantically allows the root ObjectSet to
// be an interface base set. Result items use the `$rid` / `$primaryKey` /
// `$apiName` wire prefix.
func (h *Handler) LoadObjectsOrInterfaces(w http.ResponseWriter, r *http.Request) {
	if !requirePreviewObjectSet(w, r) {
		return
	}
	h.loadPreview(w, r)
}

// loadPreview is the shared implementation for the two preview load endpoints.
func (h *Handler) loadPreview(w http.ResponseWriter, r *http.Request) {
	var req LoadObjectSetRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ObjectSet == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectSet", nil))
		return
	}

	// Foundry V2: select is REQUIRED.
	if len(req.Select) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SelectRequired", map[string]string{
			"reason": "select is required and must be a non-empty array of property apiNames",
		}))
		return
	}

	ctx := WithOntologyScope(r.Context(), chi.URLParam(r, "ontologyApiName"))

	result, err := h.executor.Execute(ctx, req.ObjectSet)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ObjectSetFailed", map[string]string{"error": err.Error()}))
		return
	}

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

	totalCount := len(result.PrimaryKeys)
	start := offset
	if start > totalCount {
		start = totalCount
	}
	end := start + pageSize
	if end > totalCount {
		end = totalCount
	}
	pagePKs := result.PrimaryKeys[start:end]

	data := make([]map[string]interface{}, 0, len(pagePKs))
	for _, pk := range pagePKs {
		searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{pk}))
		searchReq.Fields = req.Select
		searchReq.Size = 1

		res, err := h.indexMgr.Search(scopedIndexKey(ctx, h.indexMgr, result.ObjectType), searchReq)
		if err != nil || len(res.Hits) == 0 {
			continue
		}

		props := res.Hits[0].Fields
		item := make(map[string]interface{}, len(req.Select)+3)
		for _, f := range req.Select {
			if v, ok := props[f]; ok {
				item[f] = v
			}
		}
		item["$rid"] = fmt.Sprintf("ri.phonograph2-objects.main.object.%s", pk)
		item["$primaryKey"] = pk
		item["$apiName"] = result.ObjectType
		data = append(data, item)
	}

	accuracy := "EXACT"
	if result.Truncated {
		accuracy = "APPROXIMATE"
	}
	resp := &LoadObjectSetV2MultipleObjectTypesResponse{
		Data:               data,
		TotalCount:         strconv.Itoa(totalCount),
		TotalCountAccuracy: accuracy,
	}
	if end < totalCount {
		nextCursor := &pagination.Cursor{Offset: end}
		resp.NextPageToken = nextCursor.Encode()
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// requirePreviewObjectSet checks for ?preview=true and writes a 400 if missing.
// Kept local to the objectset package to avoid a dependency on pkg/oms.
func requirePreviewObjectSet(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Get("preview") != "true" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PreviewRequired", map[string]string{
			"reason": "This endpoint requires ?preview=true",
		}))
		return false
	}
	return true
}
