package quiver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// US-483 — Quiver Sparkline 多系列预加载. The /sparklines endpoint
// collapses the dashboard's per-series fan-out into one HTTP request so
// the SPA's initial dashboard load drops from N requests to 1. The
// share-link semantics mirror /view and /data: any authenticated caller
// who knows the RID can fetch.

// SparklinesRequest is the optional POST body. seriesIds restricts the
// fan-out to a named subset (in dashboard order, not request order);
// empty / missing returns every series the dashboard declares.
type SparklinesRequest struct {
	SeriesIDs []string `json:"seriesIds,omitempty"`
}

// SparklinesResponse is the wire shape. Series order mirrors the
// dashboard config so the SPA renders rows in a stable order without
// resorting client-side.
type SparklinesResponse struct {
	RID    string             `json:"rid"`
	Series []SparklineSeries  `json:"series"`
}

// SparklineSeries carries the points for one series plus the metadata
// the chart row needs (label / color / branch). Field set intentionally
// mirrors DataSeries minus the bucketing knobs.
type SparklineSeries struct {
	ID         string            `json:"id"`
	Label      string            `json:"label"`
	Color      string            `json:"color"`
	ObjectType string            `json:"objectType"`
	PrimaryKey string            `json:"primaryKey"`
	Property   string            `json:"property"`
	Branch     string            `json:"branch,omitempty"`
	Points     []TimeSeriesPoint `json:"points"`
}

// Sparklines handles POST /api/v2/quiver/dashboards/{rid}/sparklines.
//
// Body is optional. An empty body, `{}`, or `{"seriesIds":[]}` all
// resolve to "every series in the dashboard config". A non-empty
// seriesIds restricts the fan-out — useful for the SPA "expand row"
// flow that only needs to re-fetch one series.
func (h *Handler) Sparklines(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	if h.tsReader == nil {
		apierror.WriteJSON(w, apierror.NewInternal("QuiverTimeSeriesUnavailable", map[string]string{
			"reason": "time series store is not configured on this deployment",
		}))
		return
	}

	dashboardRID := chi.URLParam(r, "rid")
	row, err := h.store.GetByRID(r.Context(), dashboardRID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("QuiverDashboardNotFound", map[string]string{"rid": dashboardRID}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("QuiverDashboardLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}

	req, perr := parseSparklinesRequest(r.Body)
	if perr != nil {
		apierror.WriteJSON(w, perr)
		return
	}

	var cfg dashboardConfig
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("QuiverDashboardConfigInvalid", map[string]string{
				"reason": err.Error(),
			}))
			return
		}
	}

	filter := buildSeriesIDFilter(req.SeriesIDs)
	out := SparklinesResponse{
		RID:    dashboardRID,
		Series: make([]SparklineSeries, 0, len(cfg.Series)),
	}
	for _, s := range cfg.Series {
		if filter != nil && !filter[s.ID] {
			continue
		}
		key := TimeSeriesKey{
			Ontology:   cfg.OntologyAPIName,
			ObjectType: s.ObjectType,
			PrimaryKey: s.PrimaryKey,
			Property:   s.Property,
		}
		raw, err := h.tsReader.StreamPoints(r.Context(), key)
		if err != nil {
			apierror.WriteJSON(w, apierror.NewInternal("QuiverTimeSeriesReadFailed", map[string]string{
				"reason":     err.Error(),
				"seriesId":   s.ID,
				"objectType": s.ObjectType,
				"primaryKey": s.PrimaryKey,
				"property":   s.Property,
			}))
			return
		}
		if raw == nil {
			raw = []TimeSeriesPoint{}
		}
		out.Series = append(out.Series, SparklineSeries{
			ID:         s.ID,
			Label:      s.Label,
			Color:      s.Color,
			ObjectType: s.ObjectType,
			PrimaryKey: s.PrimaryKey,
			Property:   s.Property,
			Branch:     s.Branch,
			Points:     raw,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// parseSparklinesRequest tolerates an empty body, `{}`, or a proper
// SparklinesRequest. Anything else (e.g. malformed JSON) yields a
// structured 400 so the SPA surfaces a real error rather than silently
// fetching every series.
func parseSparklinesRequest(body io.ReadCloser) (SparklinesRequest, *apierror.APIError) {
	var req SparklinesRequest
	if body == nil {
		return req, nil
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		return req, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		})
	}
	if len(raw) == 0 {
		return req, nil
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return req, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		})
	}
	return req, nil
}

// buildSeriesIDFilter returns nil when ids is empty (meaning "include
// every series"), or a set used to filter the dashboard's series in
// declaration order so the response order matches the dashboard, not
// the request body.
func buildSeriesIDFilter(ids []string) map[string]bool {
	if len(ids) == 0 {
		return nil
	}
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		out[id] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
