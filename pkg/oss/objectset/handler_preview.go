package objectset

import (
	"context"
	"fmt"
	"net/http"
	"sort"
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

	// Interface-base results drive composite-cursor heap-merge pagination
	// so the per-type sub-streams can be paged and exhausted independently.
	// Non-interface results fall through to the simple offset path.
	if result.PerTypePKs != nil {
		h.writePreviewInterfacePage(w, ctx, req, result, pageSize)
		return
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

// writePreviewInterfacePage paginates a polymorphic interfaceBase Result using
// heap-merge over the executor-provided per-type buckets. Per-type offsets
// live in a MultiTypeCursor; exhausted sub-cursors are dropped so the wire
// token stays minimal. Each emitted row is fetched from its own ObjectType's
// Bleve index so $apiName / property lookup stays per-row accurate.
func (h *Handler) writePreviewInterfacePage(w http.ResponseWriter, ctx context.Context, req LoadObjectSetRequest, result *Result, pageSize int) {
	// Reconstruct per-type offsets from the inbound cursor. Any type missing
	// from SubCursors is treated as exhausted (offset == len(bucket)).
	offsets := make(map[string]int, len(result.PerTypePKs))
	if req.PageToken != "" {
		mc, err := pagination.DecodeMultiTypeCursor(req.PageToken)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPageToken", map[string]string{"error": err.Error()}))
			return
		}
		live := make(map[string]bool, len(mc.SubCursors))
		for _, sc := range mc.SubCursors {
			live[sc.ObjectType] = true
			if sc.IsExhausted() {
				offsets[sc.ObjectType] = len(result.PerTypePKs[sc.ObjectType])
				continue
			}
			n, err := strconv.Atoi(sc.InnerCursor)
			if err != nil || n < 0 {
				apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPageToken", map[string]string{
					"error": fmt.Sprintf("invalid sub-cursor for %q: %v", sc.ObjectType, sc.InnerCursor),
				}))
				return
			}
			if n > len(result.PerTypePKs[sc.ObjectType]) {
				n = len(result.PerTypePKs[sc.ObjectType])
			}
			offsets[sc.ObjectType] = n
		}
		for t, bucket := range result.PerTypePKs {
			if !live[t] {
				offsets[t] = len(bucket)
			}
		}
	}

	// Deterministic type ordering so heap-merge ties resolve stably and the
	// emitted MultiTypeCursor is comparable across requests.
	types := make([]string, 0, len(result.PerTypePKs))
	for t := range result.PerTypePKs {
		types = append(types, t)
	}
	sort.Strings(types)

	type pageRow struct {
		pk         string
		objectType string
	}
	page := make([]pageRow, 0, pageSize)
	for len(page) < pageSize {
		chosen := ""
		var chosenPK string
		for _, t := range types {
			bucket := result.PerTypePKs[t]
			idx := offsets[t]
			if idx >= len(bucket) {
				continue
			}
			pk := bucket[idx]
			if chosen == "" || pk < chosenPK || (pk == chosenPK && t < chosen) {
				chosen = t
				chosenPK = pk
			}
		}
		if chosen == "" {
			break
		}
		page = append(page, pageRow{pk: chosenPK, objectType: chosen})
		offsets[chosen]++
	}

	data := make([]map[string]interface{}, 0, len(page))
	for _, row := range page {
		searchReq := bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{row.pk}))
		searchReq.Fields = req.Select
		searchReq.Size = 1

		res, err := h.indexMgr.Search(scopedIndexKey(ctx, h.indexMgr, row.objectType), searchReq)
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
		item["$rid"] = fmt.Sprintf("ri.phonograph2-objects.main.object.%s", row.pk)
		item["$primaryKey"] = row.pk
		item["$apiName"] = row.objectType
		data = append(data, item)
	}

	totalCount := 0
	for _, bucket := range result.PerTypePKs {
		totalCount += len(bucket)
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

	// Emit a MultiTypeCursor containing only the non-exhausted sub-cursors.
	// Exhausted sub-cursors are dropped so the wire token stays compact.
	subs := make([]pagination.CompositeCursor, 0, len(types))
	for _, t := range types {
		bucket := result.PerTypePKs[t]
		idx := offsets[t]
		if idx >= len(bucket) {
			continue
		}
		subs = append(subs, pagination.CompositeCursor{
			ObjectType:  t,
			InnerCursor: strconv.Itoa(idx),
		})
	}
	if len(subs) > 0 {
		mc := &pagination.MultiTypeCursor{SubCursors: subs}
		resp.NextPageToken = mc.Encode()
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
