package oss

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/where"
)

// Handler handles OSS HTTP requests.
type Handler struct {
	svc Service
	// historyRepo, when non-nil, enables the
	// /objects/{objectType}/{primaryKey}/history endpoint. Wired via
	// SetHistoryRepo from main.go after construction so existing callers
	// of NewHandler keep their two-argument signature.
	historyRepo oms.Repository
	// broadcast, when non-nil, enables the SSE /subscribe endpoint. Wired
	// via SetBroadcast from main.go after construction. When nil, the
	// route still registers but returns 503 so callers can detect feature
	// absence.
	broadcast *funnel.Broadcast
}

// NewHandler creates a new OSS HTTP handler.
func NewHandler(svc Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes registers the OSS routes on the given chi router.
func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}", h.ListObjects)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}", h.GetObject)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/search", h.SearchObjects)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/links/{linkType}", h.ListLinkedObjects)

	// Object change history (Tier 2.3). The route is always registered so
	// the OpenAPI contract test stays in sync; the handler returns a 5xx
	// when the underlying history repo has not been wired.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/history", h.GetObjectHistory)

	// Tier 3.5 SSE subscribe endpoint. Always registered; returns 503 when
	// the broadcast hub has not been wired (degraded mode).
	r.Get("/api/v2/ontologies/{ontologyApiName}/subscribe", h.SubscribeChanges)
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
