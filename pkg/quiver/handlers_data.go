package quiver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// US-482: multi-series time-series fetch surface for a saved Quiver
// dashboard. One HTTP round-trip replaces the per-series fan-out the
// frontend used to do; the next iteration (US-483) batches sparklines
// on top of the same handler.

// TimeSeriesPoint is one (time, value) pair returned by the underlying
// timeseries reader. The shape mirrors pkg/timeseries.Point so the
// production adapter is a thin field-rename rather than a copy.
type TimeSeriesPoint struct {
	Time  time.Time   `json:"time"`
	Value interface{} `json:"value"`
}

// TimeSeriesKey uniquely identifies a series in the reader. Identical
// shape to pkg/timeseries.SeriesKey; kept local so the quiver package
// does not import the timeseries package directly. (Same dep-direction
// trick as dashboards.Store / quiver.Store: narrow capability interface
// in the consuming package, prod adapter in cmd/server.)
type TimeSeriesKey struct {
	Ontology   string
	ObjectType string
	PrimaryKey string
	Property   string
}

// TimeSeriesReader is the narrow capability interface the /data handler
// needs from the timeseries store. Wired via SetTimeSeriesReader so
// degraded-mode (no PG) deployments leave it nil and /data responds
// 5xx QuiverTimeSeriesUnavailable rather than crashing.
type TimeSeriesReader interface {
	StreamPoints(ctx context.Context, key TimeSeriesKey) ([]TimeSeriesPoint, error)
}

// dashboardSeriesConfig is the per-series shape inside a Dashboard's
// Config envelope (see web/src/api/quiver.ts::QuiverSeriesConfig). The
// /data handler only needs the fields required to (a) name the series
// in the response and (b) resolve the underlying TimeSeriesKey.
type dashboardSeriesConfig struct {
	ID         string `json:"id"`
	ObjectType string `json:"objectType"`
	PrimaryKey string `json:"primaryKey"`
	Property   string `json:"property"`
	Label      string `json:"label"`
	Color      string `json:"color"`
	Branch     string `json:"branch,omitempty"`
}

type dashboardConfig struct {
	OntologyAPIName string                  `json:"ontologyApiName"`
	Series          []dashboardSeriesConfig `json:"series"`
}

// DataResponse is the GET /api/v2/quiver/dashboards/{rid}/data wire
// shape. From / To / Step echo the resolved request parameters so the
// SPA can detect drift between the user's TopBar selection and the
// data window the server actually scanned.
type DataResponse struct {
	RID    string       `json:"rid"`
	From   time.Time    `json:"from"`
	To     time.Time    `json:"to"`
	Step   string       `json:"step"`
	Series []DataSeries `json:"series"`
}

// DataSeries is one series block in DataResponse.Series. Points carry
// the bucketed (time_bucket avg per `step`) reduction of the source
// series clipped to [From, To].
type DataSeries struct {
	ID         string            `json:"id"`
	Label      string            `json:"label"`
	Color      string            `json:"color"`
	ObjectType string            `json:"objectType"`
	PrimaryKey string            `json:"primaryKey"`
	Property   string            `json:"property"`
	Branch     string            `json:"branch,omitempty"`
	Points     []TimeSeriesPoint `json:"points"`
}

// SetTimeSeriesReader wires the underlying timeseries reader. Idempotent
// and safe to call before RegisterRoutes — the data handler reads the
// field on each request so a later call replaces the previous wiring.
func (h *Handler) SetTimeSeriesReader(r TimeSeriesReader) {
	h.tsReader = r
}

// Data handles GET /api/v2/quiver/dashboards/{rid}/data?from=&to=&step=.
//
// Share-link semantics: any authenticated caller who knows the RID can
// read the dashboard's data, mirroring the read-only `/view` route.
// Owner-scoped enumeration stays on List.
func (h *Handler) Data(w http.ResponseWriter, r *http.Request) {
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

	from, to, step, perr := parseDataParams(r.URL.Query())
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

	out := DataResponse{
		RID:    dashboardRID,
		From:   from,
		To:     to,
		Step:   r.URL.Query().Get("step"),
		Series: make([]DataSeries, 0, len(cfg.Series)),
	}
	for _, s := range cfg.Series {
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
		out.Series = append(out.Series, DataSeries{
			ID:         s.ID,
			Label:      s.Label,
			Color:      s.Color,
			ObjectType: s.ObjectType,
			PrimaryKey: s.PrimaryKey,
			Property:   s.Property,
			Branch:     s.Branch,
			Points:     bucketAvg(raw, from, to, step),
		})
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// parseDataParams pulls `from`, `to`, and `step` off the request and
// validates them. Returns a structured APIError ready to write on the
// 400 path so each branch stays one line at the call site.
func parseDataParams(q map[string][]string) (time.Time, time.Time, time.Duration, *apierror.APIError) {
	fromStr := firstValue(q, "from")
	toStr := firstValue(q, "to")
	stepStr := strings.TrimSpace(firstValue(q, "step"))

	if stepStr == "" {
		return time.Time{}, time.Time{}, 0, apierror.NewInvalidParameter("QuiverDataInvalidStep", map[string]string{
			"reason": "step is required (duration string, e.g. 5m, 1h)",
		})
	}
	step, err := time.ParseDuration(stepStr)
	if err != nil || step <= 0 {
		return time.Time{}, time.Time{}, 0, apierror.NewInvalidParameter("QuiverDataInvalidStep", map[string]string{
			"reason": "step must be a positive Go duration string (e.g. 5m, 1h)",
			"step":   stepStr,
		})
	}
	from, ferr := parseTimeParam(fromStr)
	if ferr != nil {
		return time.Time{}, time.Time{}, 0, apierror.NewInvalidParameter("QuiverDataInvalidFrom", map[string]string{
			"reason": ferr.Error(),
			"from":   fromStr,
		})
	}
	to, terr := parseTimeParam(toStr)
	if terr != nil {
		return time.Time{}, time.Time{}, 0, apierror.NewInvalidParameter("QuiverDataInvalidTo", map[string]string{
			"reason": terr.Error(),
			"to":     toStr,
		})
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		return time.Time{}, time.Time{}, 0, apierror.NewInvalidParameter("QuiverDataInvalidWindow", map[string]string{
			"reason": "to must be >= from",
			"from":   fromStr,
			"to":     toStr,
		})
	}
	return from, to, step, nil
}

// parseTimeParam accepts an empty string ("all time" sentinel), an
// RFC3339 timestamp, or a unix millisecond epoch. Mirrors the wire
// flexibility of the existing /api/v2/.../timeseries handlers so the
// SPA can pass either shape without a conversion step.
func parseTimeParam(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if ms, err := strconv.ParseInt(s, 10, 64); err == nil {
		return time.Unix(0, ms*int64(time.Millisecond)).UTC(), nil
	}
	return time.Time{}, errors.New("expected RFC3339 timestamp or unix millis")
}

func firstValue(q map[string][]string, key string) string {
	v := q[key]
	if len(v) == 0 {
		return ""
	}
	return v[0]
}

// bucketAvg clips raw to [from, to] (open at the upper end when to is
// non-zero, so a 24h window with 5m step returns exactly 24*60/5 = 288
// buckets) and reduces each step bucket to its arithmetic mean. Non-numeric
// values are skipped — the response surface is intentionally numeric.
//
// Bucket alignment: when from is supplied, bucket starts align to from
// (so the first bucket is [from, from+step)). When from is zero, we fall
// back to UTC-epoch truncation, matching applyResample.
func bucketAvg(raw []TimeSeriesPoint, from, to time.Time, step time.Duration) []TimeSeriesPoint {
	if len(raw) == 0 || step <= 0 {
		return []TimeSeriesPoint{}
	}
	type bucket struct {
		start time.Time
		sum   float64
		count int
	}
	buckets := map[int64]*bucket{}
	for _, p := range raw {
		t := p.Time.UTC()
		if !from.IsZero() && t.Before(from) {
			continue
		}
		if !to.IsZero() && !t.Before(to) {
			continue
		}
		v, ok := numericFloat(p.Value)
		if !ok {
			continue
		}
		var start time.Time
		if from.IsZero() {
			start = t.Truncate(step)
		} else {
			delta := t.Sub(from)
			n := delta / step
			start = from.Add(n * step)
		}
		key := start.UnixNano()
		b, ok := buckets[key]
		if !ok {
			b = &bucket{start: start}
			buckets[key] = b
		}
		b.sum += v
		b.count++
	}
	out := make([]TimeSeriesPoint, 0, len(buckets))
	for _, b := range buckets {
		out = append(out, TimeSeriesPoint{
			Time:  b.start,
			Value: b.sum / float64(b.count),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.Before(out[j].Time) })
	return out
}

// numericFloat coerces a JSON-decoded value to float64, mirroring
// pkg/timeseries.numericValue so MemoryStore int values and VMStore
// float64 values aggregate identically.
func numericFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}
