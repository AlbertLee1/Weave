package objectset

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/blevesearch/bleve/v2"
	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/pagination"
	"github.com/liyang/weave/pkg/oss/where"
)

// GetObjectSet handles GET /api/v2/ontologies/{o}/objectSets/{objectSetRid}.
// Returns the ObjectSet definition (not data) for a previously stored temporary ObjectSet.
func (h *Handler) GetObjectSet(w http.ResponseWriter, r *http.Request) {
	objectSetRid := chi.URLParam(r, "objectSetRid")

	def, err := h.store.Get(objectSetRid)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewNotFound("ObjectSetNotFound", map[string]string{
			"objectSetRid": objectSetRid,
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, def)
}

// LoadLinksRequest is the Foundry V2 request for loading links from an ObjectSet.
type LoadLinksRequest struct {
	ObjectSet       *Definition `json:"objectSet"`
	LinkTypeAPIName string      `json:"linkTypeApiName"`
	Select          []string    `json:"select,omitempty"`
	PageSize        int         `json:"pageSize,omitempty"`
	PageToken       string      `json:"pageToken,omitempty"`
}

// LoadLinks handles POST /api/v2/ontologies/{o}/objectSets/loadLinks.
// Executes an ObjectSet, resolves links via the link type, and returns linked objects.
func (h *Handler) LoadLinks(w http.ResponseWriter, r *http.Request) {
	var req LoadLinksRequest
	if err := httputil.ReadJSON(r, &req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{"error": err.Error()}))
		return
	}

	if req.ObjectSet == nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingObjectSet", nil))
		return
	}

	if req.LinkTypeAPIName == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingLinkTypeApiName", nil))
		return
	}

	// Build a searchAround definition to resolve links via the executor.
	searchAroundDef := &Definition{
		Type:      "searchAround",
		ObjectSet: req.ObjectSet,
		Link:      req.LinkTypeAPIName,
	}

	ctx := WithOntologyScope(r.Context(), chi.URLParam(r, "ontologyApiName"))

	result, err := h.executor.Execute(ctx, searchAroundDef)
	if err != nil {
		// Round 37: sentinel routing — user-side definition / where
		// errors stay 400 InvalidObjectSet; everything else is server-
		// side → 500 LoadLinksFailed.
		if errors.Is(err, ErrInvalidObjectSetDefinition) || errors.Is(err, where.ErrInvalidWhereClause) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidObjectSet", map[string]string{"error": err.Error()}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("LoadLinksFailed", map[string]string{"error": err.Error()}))
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

		data = append(data, oss.FormatObject(result.ObjectType, pk, props))
	}

	// US-048: honour column-level policy on the link target type.
	data, err = h.applyPropertyVisibility(ctx, result.ObjectType, data)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInternal("PropertyFilterFailed", map[string]string{"error": err.Error()}))
		return
	}

	resp := &LoadObjectSetResponse{
		Data:       data,
		TotalCount: strconv.Itoa(totalCount),
	}

	if result.Truncated {
		resp.TotalCountAccuracy = "APPROXIMATE"
	} else {
		resp.TotalCountAccuracy = "EXACT"
	}

	if end < totalCount {
		nextCursor := &pagination.Cursor{Offset: end}
		resp.NextPageToken = nextCursor.Encode()
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}
