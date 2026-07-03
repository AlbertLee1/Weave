package oss

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/timeseries"
)

// SetTimeSeriesStore wires the time series store so the handler can serve
// the object-path TimeSeriesProperty endpoints. When nil, those routes
// return TimeSeriesStoreNotConfigured.
func (h *Handler) SetTimeSeriesStore(store timeseries.Store) {
	h.timeseriesStore = store
}

// GetTimeSeriesFirstPoint handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/timeseries/{property}/firstPoint.
func (h *Handler) GetTimeSeriesFirstPoint(w http.ResponseWriter, r *http.Request) {
	key, ok := h.resolveTimeSeriesKey(w, r)
	if !ok {
		return
	}
	p, err := h.timeseriesStore.FirstPoint(r.Context(), key)
	if err != nil {
		writeTimeSeriesError(w, err)
		return
	}
	writeJSONOK(w, p)
}

// GetTimeSeriesLastPoint handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/timeseries/{property}/lastPoint.
func (h *Handler) GetTimeSeriesLastPoint(w http.ResponseWriter, r *http.Request) {
	key, ok := h.resolveTimeSeriesKey(w, r)
	if !ok {
		return
	}
	p, err := h.timeseriesStore.LastPoint(r.Context(), key)
	if err != nil {
		writeTimeSeriesError(w, err)
		return
	}
	writeJSONOK(w, p)
}

// StreamTimeSeriesPoints handles
// POST /api/v2/ontologies/{o}/objects/{type}/{pk}/timeseries/{property}/streamPoints.
//
// The ?format= query parameter mirrors Foundry's JSON/ARROW toggle. This
// implementation emits JSON only; ARROW returns 400 UnsupportedFormat.
func (h *Handler) StreamTimeSeriesPoints(w http.ResponseWriter, r *http.Request) {
	h.streamTimeSeries(w, r)
}

// GetTimeSeriesLatestValue handles
// GET /api/v2/ontologies/{o}/objects/{type}/{pk}/timeseries/{propertyName}/latestValue.
//
// TimeSeriesValueBankProperty endpoint (US-038). Returns the most recent
// point in the series, equivalent to LastPoint for TimeSeriesProperty.
// The path parameter is {propertyName} (not {property}) — this matches
// Foundry's API exactly.
func (h *Handler) GetTimeSeriesLatestValue(w http.ResponseWriter, r *http.Request) {
	key, ok := h.resolveTimeSeriesKey(w, r)
	if !ok {
		return
	}
	p, err := h.timeseriesStore.LastPoint(r.Context(), key)
	if err != nil {
		writeTimeSeriesError(w, err)
		return
	}
	writeJSONOK(w, p)
}

// StreamTimeSeriesValues handles
// POST /api/v2/ontologies/{o}/objects/{type}/{pk}/timeseries/{property}/streamValues.
//
// TimeSeriesValueBankProperty endpoint (US-038). Returns all points in
// the series, equivalent to streamPoints. Honours the same ?format=
// JSON/ARROW toggle — ARROW returns 400 UnsupportedFormat.
func (h *Handler) StreamTimeSeriesValues(w http.ResponseWriter, r *http.Request) {
	h.streamTimeSeries(w, r)
}

// streamTimeSeries implements the shared body of StreamTimeSeriesPoints
// (TimeSeriesProperty) and StreamTimeSeriesValues (TimeSeriesValueBank).
// Foundry splits these by property kind but the wire shape is identical
// — both emit an ordered []TimeSeriesPoint and reject non-JSON formats.
//
// The optional request body is Foundry's StreamTimeSeriesPointsRequest:
// {range?: TimeRange, aggregate?: AggregateTimeSeries}. `range` filters the
// returned points to a time window (absolute ISO 8601 bounds or relative
// offsets from now); `aggregate` downsamples into fixed-width periodic
// buckets. An empty body streams the full series (backward compatible).
func (h *Handler) streamTimeSeries(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format != "" && format != "JSON" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("UnsupportedFormat", map[string]string{
			"format": format,
			"reason": "only JSON format is supported on this server",
		}))
		return
	}
	key, ok := h.resolveTimeSeriesKey(w, r)
	if !ok {
		return
	}

	req, err := readStreamPointsRequest(r)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesStreamInvalidBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	var window timeseries.TimeWindow
	if req.Range != nil {
		window, err = req.Range.Resolve(time.Now().UTC())
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesInvalidRange", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
	}

	if req.Aggregate != nil {
		spec, err := req.Aggregate.Resolve()
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesInvalidAggregate", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		if window.HasStart {
			spec.Start = window.Start
		}
		if window.HasEnd {
			spec.End = window.End
		}
		h.writeDownsampled(r.Context(), w, key, spec)
		return
	}

	points, err := h.timeseriesStore.StreamPoints(r.Context(), key)
	if err != nil {
		writeTimeSeriesError(w, err)
		return
	}
	if req.Range != nil {
		points = window.Filter(points)
	}
	if points == nil {
		points = []timeseries.Point{}
	}
	writeJSONOK(w, points)
}

// writeDownsampled resolves the aggregate request. When the store can push
// the reduce down (implements timeseries.Downsampler) it does so — bounded
// wire payload regardless of series cardinality (US-435). Otherwise it
// falls back to streaming the raw points and reducing in-process.
func (h *Handler) writeDownsampled(ctx context.Context, w http.ResponseWriter, key timeseries.SeriesKey, spec timeseries.DownsampleSpec) {
	if downsampler, ok := h.timeseriesStore.(timeseries.Downsampler); ok {
		points, err := downsampler.DownsamplePoints(ctx, key, spec)
		if err != nil {
			writeTimeSeriesError(w, err)
			return
		}
		if points == nil {
			points = []timeseries.Point{}
		}
		writeJSONOK(w, points)
		return
	}
	raw, err := h.timeseriesStore.StreamPoints(ctx, key)
	if err != nil {
		writeTimeSeriesError(w, err)
		return
	}
	points, err := timeseries.DownsampleInMemory(raw, spec)
	if err != nil {
		writeTimeSeriesError(w, err)
		return
	}
	if points == nil {
		points = []timeseries.Point{}
	}
	writeJSONOK(w, points)
}

// readStreamPointsRequest decodes the optional StreamTimeSeriesPointsRequest
// body. An empty body (no bytes / whitespace only) is valid and yields a
// zero request so the caller streams the full series.
func readStreamPointsRequest(r *http.Request) (timeseries.StreamPointsRequest, error) {
	var req timeseries.StreamPointsRequest
	defer r.Body.Close()
	raw, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, httputil.MaxBodySize))
	if err != nil {
		return req, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return req, nil
	}
	// Not DisallowUnknownFields: real Foundry periodic requests carry
	// windowType / alignmentTimestamp fields this single-machine server
	// does not act on (it aligns buckets to the UTC epoch); tolerating
	// them keeps forward compatibility with richer clients.
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, err
	}
	return req, nil
}

// resolveTimeSeriesKey loads the addressed object (to verify it exists and
// returns 404 for missing objects) and returns the SeriesKey derived from
// the URL parameters. It writes the appropriate error response and returns
// ok=false on any failure.
func (h *Handler) resolveTimeSeriesKey(w http.ResponseWriter, r *http.Request) (timeseries.SeriesKey, bool) {
	if h.timeseriesStore == nil {
		apierror.WriteJSON(w, apierror.NewInternal("TimeSeriesStoreNotConfigured", nil))
		return timeseries.SeriesKey{}, false
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")
	propertyName := chi.URLParam(r, "property")
	if propertyName == "" {
		propertyName = chi.URLParam(r, "propertyName")
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
			return timeseries.SeriesKey{}, false
		}
		apierror.WriteJSON(w, apierror.NewInternal("GetObjectFailed", map[string]string{
			"reason": err.Error(),
		}))
		return timeseries.SeriesKey{}, false
	}
	_ = obj // existence check only — the series key is derived from the path.

	return timeseries.SeriesKey{
		Ontology:   ontologyRID,
		ObjectType: objectType,
		PrimaryKey: primaryKey,
		Property:   propertyName,
	}, true
}

func writeTimeSeriesError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, timeseries.ErrNoPoints):
		apierror.WriteJSON(w, apierror.NewNotFound("TimeSeriesPointNotFound", nil))
	case errors.Is(err, timeseries.ErrNonNumericValue):
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesNonNumericValue", map[string]string{
			"reason": err.Error(),
		}))
	default:
		apierror.WriteJSON(w, apierror.NewInternal("TimeSeriesStoreError", map[string]string{
			"message": err.Error(),
		}))
	}
}

// AppendTimeSeriesPoint handles
// POST /api/v2/ontologies/{o}/objects/{type}/{pk}/timeseries/{property}/points.
//
// Body shape: {"time":"2026-04-01T00:00:00Z","value":42.5}. Time is RFC3339;
// value is forwarded to the configured Store as-is so the memory and PG
// backends keep accepting non-numeric payloads. The VictoriaMetrics
// backend coerces value to float64 and returns 400 TimeSeriesNonNumericValue
// for unsupported types.
func (h *Handler) AppendTimeSeriesPoint(w http.ResponseWriter, r *http.Request) {
	key, ok := h.resolveTimeSeriesKey(w, r)
	if !ok {
		return
	}
	var body struct {
		Time  string      `json:"time"`
		Value interface{} `json:"value"`
	}
	if err := httputil.ReadJSON(r, &body); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesPointInvalidBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if body.Time == "" {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesPointInvalidBody", map[string]string{
			"reason": "time is required (RFC3339)",
		}))
		return
	}
	ts, err := time.Parse(time.RFC3339Nano, body.Time)
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesPointInvalidBody", map[string]string{
			"reason": "invalid time: " + err.Error(),
		}))
		return
	}
	if err := h.timeseriesStore.AppendPoint(r.Context(), key, timeseries.Point{Time: ts, Value: body.Value}); err != nil {
		writeTimeSeriesError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
