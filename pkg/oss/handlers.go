package oss

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/geotemporal"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/aggregation"
	"github.com/liyang/weave/pkg/oss/where"
	"github.com/liyang/weave/pkg/timeseries"
)

// Handler handles OSS HTTP requests.
type Handler struct {
	svc Service
	// aggEngine and indexMgr, when both non-nil, enable the per-object-type
	// aggregation endpoint. Wired via SetAggregation from main.go after
	// construction. When nil, the route returns AggregationNotConfigured.
	aggEngine *aggregation.Engine
	indexMgr  *index.Manager
	// omsRepo provides interface resolution for /interfaces/ data query endpoints.
	// Wired via SetOmsRepo from main.go after construction.
	omsRepo oms.Repository
	// attachmentStore backs the object-path attachment read endpoints
	// (/objects/{type}/{pk}/attachments/{property}...). When nil, those
	// routes return AttachmentStoreNotConfigured. Wired via
	// SetAttachmentStore from main.go after construction.
	attachmentStore attachment.BlobStore
	// timeseriesStore backs the object-path TimeSeriesProperty endpoints
	// (/objects/{type}/{pk}/timeseries/{property}/...). When nil, those
	// routes return TimeSeriesStoreNotConfigured. Wired via
	// SetTimeSeriesStore from main.go after construction.
	timeseriesStore timeseries.Store
	// geotemporalStore backs the object-path GeotemporalSeriesProperty
	// endpoints (/objects/{type}/{pk}/geotemporalSeries/{propertyName}/...).
	// When nil, those routes return GeotemporalStoreNotConfigured. Wired
	// via SetGeotemporalStore from main.go after construction.
	geotemporalStore geotemporal.Store
}

// NewHandler creates a new OSS HTTP handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// SetOmsRepo wires the OMS repository for interface data query endpoints.
// These endpoints need to resolve interfaces to their implementing ObjectTypes.
func (h *Handler) SetOmsRepo(repo oms.Repository) {
	h.omsRepo = repo
}

// RegisterRoutes registers the OSS routes on the given chi router.
// Only Foundry-aligned endpoints remain; history and SSE subscribe
// were removed in US-006.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}", h.ListObjects)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}", h.GetObject)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/search", h.SearchObjects)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/aggregate", h.AggregateObjects)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/count", h.CountObjects)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/links/{linkType}/{linkedObjectPrimaryKey}", h.GetLinkedObject)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/links/{linkType}", h.ListLinkedObjects)

	// AttachmentProperty read endpoints (Foundry OSv2). The static /content
	// segment must come before the wildcard {attachmentRid} so chi resolves
	// /attachments/{property}/content correctly; and the longest path
	// (/{attachmentRid}/content) must come before /{attachmentRid} for the
	// same reason.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/attachments/{property}/content", h.GetAttachmentPropertyContent)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/attachments/{property}/{attachmentRid}/content", h.GetAttachmentPropertyContentByRID)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/attachments/{property}/{attachmentRid}", h.GetAttachmentPropertyMetadataByRID)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/attachments/{property}", h.GetAttachmentPropertyMetadata)

	// MediaReferenceProperty endpoints (Foundry OSv2). Reuses the attachment
	// BlobStore under the hood. Upload lives on /objectTypes/{objectType},
	// while read endpoints address a specific object/primaryKey.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/media/{property}/metadata", h.GetMediaPropertyMetadata)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/media/{property}/content", h.GetMediaPropertyContent)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objectTypes/{objectType}/media/{property}/upload", h.UploadMediaProperty)

	// TimeSeriesProperty endpoints (Foundry OSv2). Read endpoints resolve
	// a SeriesKey from the object/primaryKey/property path segments.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}/firstPoint", h.GetTimeSeriesFirstPoint)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}/lastPoint", h.GetTimeSeriesLastPoint)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}/streamPoints", h.StreamTimeSeriesPoints)

	// TimeSeriesValueBankProperty endpoints (US-038). The {propertyName}
	// vs {property} path parameter inconsistency is deliberate — it
	// matches Foundry's OpenAPI exactly. resolveTimeSeriesKey accepts
	// either chi URL param.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{propertyName}/latestValue", h.GetTimeSeriesLatestValue)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}/streamValues", h.StreamTimeSeriesValues)

	// GeotemporalSeriesProperty endpoints (US-039). Distinct /geotemporalSeries/
	// path prefix from TimeSeriesProperty; wire shape is GeotemporalSeriesValue
	// {time, position} where position is a GeoJSON Point.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/geotemporalSeries/{propertyName}/latestValue", h.GetGeotemporalLatestValue)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/geotemporalSeries/{propertyName}/streamHistoricValues", h.StreamGeotemporalHistoricValues)

	// Interface data query endpoints (Foundry dual prefix: /interfaces/ for data, /interfaceTypes/ for metadata)
	r.Post("/api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceType}/search", h.InterfaceSearchObjects)
	r.Post("/api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceType}/aggregate", h.InterfaceAggregateObjects)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceType}/{objectType}/{primaryKey}/links/{interfaceLinkType}", h.InterfaceListLinkedObjects)
	r.Get("/api/v2/ontologies/{ontologyApiName}/interfaces/{interfaceType}", h.InterfaceListObjects)
}

// GetObject handles GET /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}.
func (h *Handler) GetObject(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")

	obj, err := h.svc.GetObject(r.Context(), GetObjectRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
		PrimaryKey:  primaryKey,
	})
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectNotFound", map[string]string{
				"objectType": objectType,
				"primaryKey": primaryKey,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("GetObjectFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, obj)
}

// ListObjects handles GET /api/v2/ontologies/{ontologyApiName}/objects/{objectType}.
func (h *Handler) ListObjects(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")

	pageSize := 0
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		var err error
		pageSize, err = strconv.Atoi(ps)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPageSize", map[string]string{
				"pageSize": ps,
			}))
			return
		}
	}

	page, err := h.svc.ListObjects(r.Context(), ListObjectsRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
		PageSize:    pageSize,
		PageToken:   r.URL.Query().Get("pageToken"),
		OrderBy:     r.URL.Query().Get("orderBy"),
	})
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectType": objectType,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ListObjectsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, page)
}

// OrderBy specifies ordering for search results.
type OrderBy struct {
	Fields []OrderByField `json:"fields,omitempty"`
}

// OrderByField specifies a single field ordering.
type OrderByField struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

// searchRequestBody is the JSON body for search requests.
type searchRequestBody struct {
	Where     *where.WhereClause `json:"where"`
	Select    []string           `json:"select,omitempty"`
	PageSize  int                `json:"pageSize,omitempty"`
	PageToken string             `json:"pageToken,omitempty"`
	OrderBy   *OrderBy           `json:"orderBy,omitempty"`
}

// SearchObjects handles POST /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/search.
func (h *Handler) SearchObjects(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")

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

	// Read pageSize from body first, fall back to query params.
	pageSize := body.PageSize
	if pageSize == 0 {
		if ps := r.URL.Query().Get("pageSize"); ps != "" {
			pageSize, _ = strconv.Atoi(ps)
		}
	}

	// Read pageToken from body first, fall back to query params.
	pageToken := body.PageToken
	if pageToken == "" {
		pageToken = r.URL.Query().Get("pageToken")
	}

	// Read orderBy from query params (kept for backwards compat).
	orderBy := r.URL.Query().Get("orderBy")

	page, err := h.svc.SearchObjects(r.Context(), SearchObjectsRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
		Where:       body.Where,
		PageSize:    pageSize,
		PageToken:   pageToken,
		OrderBy:     orderBy,
	})
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectType": objectType,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("SearchObjectsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, page)
}

// CountObjects handles POST /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/count.
func (h *Handler) CountObjects(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")

	resp, err := h.svc.CountObjects(r.Context(), CountObjectsRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
	})
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectTypeNotFound", map[string]string{
				"objectType": objectType,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("CountObjectsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, resp)
}

// GetLinkedObject handles GET /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/links/{linkType}/{linkedObjectPrimaryKey}.
func (h *Handler) GetLinkedObject(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")
	linkType := chi.URLParam(r, "linkType")
	linkedObjectPK := chi.URLParam(r, "linkedObjectPrimaryKey")

	obj, err := h.svc.GetLinkedObject(r.Context(), GetLinkedObjectRequest{
		OntologyRID:            ontologyRID,
		ObjectType:             objectType,
		PrimaryKey:             primaryKey,
		LinkType:               linkType,
		LinkedObjectPrimaryKey: linkedObjectPK,
	})
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("LinkedObjectNotFound", map[string]string{
				"objectType":             objectType,
				"primaryKey":             primaryKey,
				"linkType":               linkType,
				"linkedObjectPrimaryKey": linkedObjectPK,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("GetLinkedObjectFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, obj)
}

// ListLinkedObjects handles GET /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/links/{linkType}.
func (h *Handler) ListLinkedObjects(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")
	linkType := chi.URLParam(r, "linkType")

	pageSize := 0
	if ps := r.URL.Query().Get("pageSize"); ps != "" {
		var err error
		pageSize, err = strconv.Atoi(ps)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidPageSize", map[string]string{
				"pageSize": ps,
			}))
			return
		}
	}

	page, err := h.svc.ListLinkedObjects(r.Context(), LinkedObjectsRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
		PrimaryKey:  primaryKey,
		LinkType:    linkType,
		Direction:   r.URL.Query().Get("direction"),
		PageSize:    pageSize,
		PageToken:   r.URL.Query().Get("pageToken"),
	})
	if err != nil {
		if errors.Is(err, oms.ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectNotFound", map[string]string{
				"objectType": objectType,
				"primaryKey": primaryKey,
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInvalidParameter("ListLinkedObjectsFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, page)
}
