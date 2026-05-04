package oss

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/timeseries"
)

// TransformTimeSeries handles
// POST /api/v2/ontologies/{ontologyApiName}/timeseries/transform.
//
// US-402 — Quiver chain transform endpoint. Body shape:
//
//	{
//	  "source": {                    // optional; resolves a series via Store
//	    "objectType": "...",
//	    "primaryKey": "...",
//	    "property":   "..."
//	  },
//	  "points": [                    // optional; inline points if no source
//	    {"time": "RFC3339", "value": 1.5}
//	  ],
//	  "transforms": [
//	    {"op": "diff"},
//	    {"op": "sma",      "params": {"window": 5}},
//	    {"op": "ema",      "params": {"alpha": 0.3}},
//	    {"op": "resample", "params": {"interval": "1h", "agg": "avg"}},
//	    {"op": "scale",    "params": {"factor": 2.0, "offset": 0}}
//	  ]
//	}
//
// Exactly one of source / points must be supplied. transforms is
// required and applied in order. Response: `{"points":[…]}`.
func (h *Handler) TransformTimeSeries(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source *struct {
			ObjectType string `json:"objectType"`
			PrimaryKey string `json:"primaryKey"`
			Property   string `json:"property"`
		} `json:"source,omitempty"`
		Points     []timeseries.Point        `json:"points,omitempty"`
		Transforms []timeseries.TransformSpec `json:"transforms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesTransformInvalidBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if len(body.Transforms) == 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesTransformInvalidBody", map[string]string{
			"reason": "transforms is required",
		}))
		return
	}
	if (body.Source == nil) == (len(body.Points) == 0) {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesTransformInvalidBody", map[string]string{
			"reason": "exactly one of source / points must be supplied",
		}))
		return
	}

	points := body.Points
	if body.Source != nil {
		if h.timeseriesStore == nil {
			apierror.WriteJSON(w, apierror.NewInternal("TimeSeriesStoreNotConfigured", nil))
			return
		}
		ontologyRID := chi.URLParam(r, "ontologyApiName")
		if body.Source.ObjectType == "" || body.Source.PrimaryKey == "" || body.Source.Property == "" {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesTransformInvalidBody", map[string]string{
				"reason": "source.objectType, source.primaryKey, source.property are required",
			}))
			return
		}
		if _, err := h.svc.GetObject(r.Context(), GetObjectRequest{
			OntologyRID: ontologyRID,
			ObjectType:  body.Source.ObjectType,
			PrimaryKey:  body.Source.PrimaryKey,
		}); err != nil {
			if errors.Is(err, oms.ErrNotFound) {
				apierror.WriteJSON(w, apierror.NewNotFound("ObjectNotFound", map[string]string{
					"objectType": body.Source.ObjectType,
					"primaryKey": body.Source.PrimaryKey,
				}))
				return
			}
			apierror.WriteJSON(w, apierror.NewInvalidParameter("GetObjectFailed", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		key := timeseries.SeriesKey{
			Ontology:   ontologyRID,
			ObjectType: body.Source.ObjectType,
			PrimaryKey: body.Source.PrimaryKey,
			Property:   body.Source.Property,
		}
		// US-435: when the chain is a single resample step AND the
		// store can downsample server-side, push the bucket reduce
		// down. For a 100M-point series this turns a multi-second
		// stream into a constant-time HTTP round-trip; the response
		// payload is bounded by the downsampled bucket count, not
		// raw cardinality.
		if downsampler, spec, ok := pushDownDownsample(h.timeseriesStore, body.Transforms); ok {
			downsampled, err := downsampler.DownsamplePoints(r.Context(), key, spec)
			if err != nil {
				writeTimeSeriesError(w, err)
				return
			}
			if downsampled == nil {
				downsampled = []timeseries.Point{}
			}
			writeJSONOK(w, map[string]interface{}{"points": downsampled})
			return
		}
		fetched, err := h.timeseriesStore.StreamPoints(r.Context(), key)
		if err != nil {
			writeTimeSeriesError(w, err)
			return
		}
		points = fetched
	}

	out, err := timeseries.ApplyChain(points, body.Transforms)
	if err != nil {
		if errors.Is(err, timeseries.ErrInvalidTransform) {
			apierror.WriteJSON(w, apierror.NewInvalidParameter("TimeSeriesTransformInvalidStep", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("TimeSeriesTransformFailed", map[string]string{
			"message": err.Error(),
		}))
		return
	}
	if out == nil {
		out = []timeseries.Point{}
	}
	writeJSONOK(w, map[string]interface{}{"points": out})
}

// pushDownDownsample inspects the transform chain and the configured
// store. When the chain is exactly one `resample` step AND the store
// implements timeseries.Downsampler, it returns the downsampler, the
// translated DownsampleSpec, and true. Any other shape (multiple
// steps, a different op, malformed params, an unsupported aggregation)
// returns ok=false so the caller falls back to the streaming path.
func pushDownDownsample(store timeseries.Store, chain []timeseries.TransformSpec) (timeseries.Downsampler, timeseries.DownsampleSpec, bool) {
	if len(chain) != 1 {
		return nil, timeseries.DownsampleSpec{}, false
	}
	step := chain[0]
	if step.Op != timeseries.OpResample {
		return nil, timeseries.DownsampleSpec{}, false
	}
	intervalRaw, ok := step.Params["interval"].(string)
	if !ok {
		return nil, timeseries.DownsampleSpec{}, false
	}
	interval, err := time.ParseDuration(intervalRaw)
	if err != nil || interval <= 0 {
		return nil, timeseries.DownsampleSpec{}, false
	}
	aggName := ""
	if v, ok := step.Params["agg"].(string); ok {
		aggName = v
	}
	agg, ok := timeseries.NormalizeAggregation(aggName)
	if !ok {
		return nil, timeseries.DownsampleSpec{}, false
	}
	downsampler, ok := store.(timeseries.Downsampler)
	if !ok {
		return nil, timeseries.DownsampleSpec{}, false
	}
	return downsampler, timeseries.DownsampleSpec{
		Step:        interval,
		Aggregation: agg,
	}, true
}
