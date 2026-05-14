package oss

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/attachment"
	"github.com/liyang/weave/pkg/cipher"
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
	// cipherDecryptor backs the object-path CipherTextProperty decrypt
	// endpoint (/objects/{type}/{pk}/ciphertexts/{property}/decrypt). When
	// nil, the route returns CipherDecryptorNotConfigured. Wired via
	// SetCipherDecryptor from main.go after construction.
	cipherDecryptor cipher.Decryptor
	// propertyFilter is the optional US-049 column-level gate that
	// AggregateObjects consults to reject requests whose groupBy.field or
	// metric.field reference properties outside the caller's allow list.
	// Wired via SetPropertyFilterProvider from main.go. Nil => no gate.
	propertyFilter PropertyFilterProvider
	// activityStore backs the object-path activity-timeline endpoint
	// (/objects/{type}/{pk}/activity). When nil, the route returns
	// ActivityStoreNotConfigured. Wired via SetActivityStore from main.go
	// after construction.
	activityStore oms.ObjectActivityStore
	// scenarioReader, when non-nil, enables Vertex scenario overlay via the
	// X-Scenario-Id header on Read endpoints (VTX-004). Wired via
	// SetScenarioReader.
	scenarioReader ScenarioReader
	// vertexTSQuerier, when non-nil, powers the Vertex window-aggregation
	// timeseries endpoint (VTX-030). Wired via SetVertexTimeSeriesQuerier.
	vertexTSQuerier VertexTimeSeriesQuerier
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

	// Per-object activity timeline (US-312). Walks the object_history tail
	// for one (objectType, primaryKey) tuple with cursor-based pagination so
	// the ObjectDetail UI can render a paginated change-log without
	// over-fetching.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/activity", h.GetObjectActivity)

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

	// Vertex window-aggregation endpoint (VTX-030). Sits *before* the
	// Foundry sub-path routes so a future router that prefers
	// shortest-match doesn't accidentally swallow /timeseries/{property};
	// chi already matches longest-specific first, but ordering this
	// register first makes the intent obvious for readers.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}", h.GetVertexTimeSeries)

	// TimeSeriesProperty endpoints (Foundry OSv2). Read endpoints resolve
	// a SeriesKey from the object/primaryKey/property path segments.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}/firstPoint", h.GetTimeSeriesFirstPoint)
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}/lastPoint", h.GetTimeSeriesLastPoint)
	r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}/streamPoints", h.StreamTimeSeriesPoints)
	// US-400: write endpoint. Body is {time:RFC3339, value:any}. Routes
	// to the configured backend (memory / PG / VictoriaMetrics).
	r.Post("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}/points", h.AppendTimeSeriesPoint)

	// US-402: chain-transform endpoint. Accepts inline points or a
	// store-resolved series source plus an ordered list of
	// {op, params} steps; returns the transformed series.
	r.Post("/api/v2/ontologies/{ontologyApiName}/timeseries/transform", h.TransformTimeSeries)

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

	// CipherTextProperty decrypt endpoint (US-040). Returns DecryptionResult
	// {plaintext}. The decryptor is an envelope-encryption AES-GCM impl by
	// default; swapping in a KMS-backed Decryptor only requires providing a
	// different cipher.Decryptor through SetCipherDecryptor.
	r.Get("/api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/ciphertexts/{property}/decrypt", h.DecryptCipherTextProperty)

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

	overlay, apiErr := h.loadScenarioOverlay(r.Context(), r, ontologyRID)
	if apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

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

	if overlay != nil {
		overlaid, deleted := overlay.applyToObject(obj)
		if deleted {
			apierror.WriteJSON(w, apierror.NewNotFound("ObjectNotFound", map[string]string{
				"objectType": objectType,
				"primaryKey": primaryKey,
				"reason":     "deleted in scenario",
			}))
			return
		}
		obj = overlaid
	}

	httputil.WriteJSON(w, http.StatusOK, obj)
}

// ListObjects handles GET /api/v2/ontologies/{ontologyApiName}/objects/{objectType}.
func (h *Handler) ListObjects(w http.ResponseWriter, r *http.Request) {
	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")

	overlay, apiErr := h.loadScenarioOverlay(r.Context(), r, ontologyRID)
	if apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

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

	if overlay != nil {
		page = overlay.applyToPage(page)
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
	Fuzzy     *where.FuzzyConfig `json:"fuzzy,omitempty"`
	Select    []string           `json:"select,omitempty"`
	Highlight *HighlightConfig   `json:"highlight,omitempty"`
	Facets    []string           `json:"facets,omitempty"`
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

	// Read fuzziness from query params. Accepts 0/1/2; anything else is a 400.
	// When present it REPLACES any body.Fuzzy — the query-string form is the
	// documented Foundry-style API and wins on conflict.
	fuzzy := body.Fuzzy
	if raw := r.URL.Query().Get("fuzziness"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > where.MaxFuzziness {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFuzziness", map[string]string{
				"reason":     "fuzziness must be 0, 1, or 2",
				"fuzziness":  raw,
				"maxAllowed": strconv.Itoa(where.MaxFuzziness),
			}))
			return
		}
		if n == 0 {
			fuzzy = nil
		} else {
			fuzzy = &where.FuzzyConfig{MaxEdits: n}
		}
	} else if fuzzy != nil && (fuzzy.MaxEdits < 0 || fuzzy.MaxEdits > where.MaxFuzziness) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFuzziness", map[string]string{
			"reason":     "fuzzy.maxEdits must be 0, 1, or 2",
			"fuzziness":  strconv.Itoa(fuzzy.MaxEdits),
			"maxAllowed": strconv.Itoa(where.MaxFuzziness),
		}))
		return
	}

	// US-234: ?regex=field:pattern shorthand. When present it REPLACES
	// body.where with a single `{type: "regex", field, value: pattern}` clause,
	// matching the fuzziness-overrides-body convention. Use the structured
	// where-clause body form (`{"type":"regex",...}`) for AND/OR composition.
	whereClause := body.Where
	if raw := r.URL.Query().Get("regex"); raw != "" {
		field, pattern, ok := splitRegexQueryParam(raw)
		if !ok {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRegex", map[string]string{
				"reason": "regex query parameter must be in field:pattern form",
				"regex":  raw,
			}))
			return
		}
		valBytes, _ := json.Marshal(pattern)
		whereClause = &where.WhereClause{
			Type:  "regex",
			Field: field,
			Value: valBytes,
		}
	}

	// US-235: ?highlight=true or ?highlight=field1,field2 shorthand. Presence
	// of the param enables highlighting regardless of body; an explicit
	// `false`/`0`/`off` disables it even if the body asked for it. When the
	// param is absent, body.Highlight (if any) wins.
	highlight := body.Highlight
	if raw := r.URL.Query().Get("highlight"); raw != "" {
		hl, ok := parseHighlightQueryParam(raw)
		if !ok {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidHighlight", map[string]string{
				"reason":    "highlight query parameter must be true/false or a comma-separated field list",
				"highlight": raw,
			}))
			return
		}
		highlight = hl
	}

	// US-236: ?facets=field1,field2 enables per-field term-count buckets.
	// When the query param is present it REPLACES body.facets so the
	// URL-only invocation stays first-class. An empty / all-whitespace
	// param is a 400 — same shape as highlight/regex.
	facets := body.Facets
	if raw, ok := r.URL.Query()["facets"]; ok && len(raw) > 0 {
		parsed, parseOK := parseFacetsQueryParam(raw[0])
		if !parseOK {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFacets", map[string]string{
				"reason": "facets query parameter must be a non-empty comma-separated field list",
				"facets": raw[0],
			}))
			return
		}
		facets = parsed
	}

	page, err := h.svc.SearchObjects(r.Context(), SearchObjectsRequest{
		OntologyRID: ontologyRID,
		ObjectType:  objectType,
		Where:       whereClause,
		Fuzzy:       fuzzy,
		Highlight:   highlight,
		Facets:      facets,
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

// parseHighlightQueryParam parses `?highlight=` values: literal booleans
// (true/1/on/yes enable with defaults, false/0/off/no explicitly disable),
// or a comma-separated field list which enables highlighting on those
// fields only. Returns (nil, true) for the explicit-disable case and
// (non-nil, true) for the enable cases. An all-whitespace / unparseable
// input returns (nil, false) so the handler can surface a 400.
func parseHighlightQueryParam(raw string) (*HighlightConfig, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, false
	}
	switch strings.ToLower(trimmed) {
	case "false", "0", "no", "off":
		return nil, true
	case "true", "1", "yes", "on":
		return &HighlightConfig{}, true
	}
	parts := strings.Split(trimmed, ",")
	fields := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			fields = append(fields, p)
		}
	}
	if len(fields) == 0 {
		return nil, false
	}
	return &HighlightConfig{Fields: fields}, true
}

// parseFacetsQueryParam splits `?facets=field1,field2,field3` into a clean
// field-name slice. Leading/trailing whitespace per field is trimmed. A
// trailing comma (`a,b,`) is tolerated — only non-empty fields survive. An
// input whose every field is empty (`""`, `",,"`, `"   "`) returns
// (nil, false) so the handler can surface a 400.
func parseFacetsQueryParam(raw string) ([]string, bool) {
	parts := strings.Split(raw, ",")
	fields := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			fields = append(fields, p)
		}
	}
	if len(fields) == 0 {
		return nil, false
	}
	return fields, true
}

// splitRegexQueryParam parses the `?regex=field:pattern` shorthand. The split
// is on the FIRST colon so patterns may legitimately contain colons (e.g.
// `^foo:bar$`). Returns false when the field part is missing/empty or the
// colon is absent — both surface as a 400 to the caller.
func splitRegexQueryParam(raw string) (field, pattern string, ok bool) {
	idx := strings.IndexByte(raw, ':')
	if idx <= 0 || idx == len(raw)-1 {
		return "", "", false
	}
	field = strings.TrimSpace(raw[:idx])
	pattern = raw[idx+1:]
	if field == "" || pattern == "" {
		return "", "", false
	}
	return field, pattern, true
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

	overlay, apiErr := h.loadScenarioOverlay(r.Context(), r, ontologyRID)
	if apiErr != nil {
		apierror.WriteJSON(w, apiErr)
		return
	}

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

	if overlay != nil {
		page = overlay.applyToPage(page)
	}

	httputil.WriteJSON(w, http.StatusOK, page)
}
