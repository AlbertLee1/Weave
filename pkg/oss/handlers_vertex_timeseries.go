package oss

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/timeseries"
)

// VertexTimeSeriesQuerier is the dependency surface VertexService satisfies
// for the VTX-030 window-aggregation endpoint. Tests can supply a fake.
type VertexTimeSeriesQuerier interface {
	Query(ctx context.Context, q timeseries.VertexQuery) (*timeseries.VertexQueryResult, error)
}

// SetVertexTimeSeriesQuerier wires the Vertex window-aggregation service.
// When unset, the VTX-030 endpoint returns VertexTimeSeriesNotConfigured.
func (h *Handler) SetVertexTimeSeriesQuerier(q VertexTimeSeriesQuerier) {
	h.vertexTSQuerier = q
}

// GetVertexTimeSeries handles
// GET /api/v2/ontologies/{ontologyApiName}/objects/{objectType}/{primaryKey}/timeseries/{property}
// with query params from=RFC3339&to=RFC3339&agg=AVG|MIN|MAX|SUM|LAST&bucket=5m&scenarioId=…
//
// This is the Vertex window-aggregation endpoint (VTX-030). It coexists with
// the Foundry-OSv2 /timeseries/{property}/firstPoint|lastPoint|streamPoints
// endpoints because chi matches the more-specific sub-paths first.
func (h *Handler) GetVertexTimeSeries(w http.ResponseWriter, r *http.Request) {
	if h.vertexTSQuerier == nil {
		apierror.WriteJSON(w, apierror.NewInternal("VertexTimeSeriesNotConfigured", nil))
		return
	}

	ontologyRID := chi.URLParam(r, "ontologyApiName")
	objectType := chi.URLParam(r, "objectType")
	primaryKey := chi.URLParam(r, "primaryKey")
	property := chi.URLParam(r, "property")
	if property == "" {
		property = chi.URLParam(r, "propertyName")
	}

	q := r.URL.Query()
	from, err := parseRFC3339Param(q.Get("from"))
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidFrom", map[string]string{"reason": err.Error()}))
		return
	}
	to, err := parseRFC3339Param(q.Get("to"))
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidTo", map[string]string{"reason": err.Error()}))
		return
	}
	agg, err := parseAggParam(q.Get("agg"))
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidAgg", map[string]string{"reason": err.Error()}))
		return
	}
	bucket, err := parseBucketParam(q.Get("bucket"))
	if err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidBucket", map[string]string{"reason": err.Error()}))
		return
	}

	res, err := h.vertexTSQuerier.Query(r.Context(), timeseries.VertexQuery{
		ObjectRID:  vertexObjectRID(ontologyRID, objectType, primaryKey),
		Property:   property,
		From:       from,
		To:         to,
		Agg:        agg,
		Bucket:     bucket,
		ScenarioID: q.Get("scenarioId"),
	})
	if err != nil {
		if errors.Is(err, timeseries.ErrScenarioNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("ScenarioNotFound", map[string]string{
				"scenarioId": q.Get("scenarioId"),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("VertexTimeSeriesQueryFailed", map[string]string{
			"message": err.Error(),
		}))
		return
	}
	writeJSONOK(w, res)
}

// vertexObjectRID composes the synthetic object_rid we store on the VTX-028
// hypertable from the URL path tuple. Production read-paths address the
// hypertable by this exact convention so the REST API can roundtrip a write
// path that uses (ontology, objectType, primaryKey) without a separate RID
// lookup.
func vertexObjectRID(ontology, objectType, primaryKey string) string {
	return fmt.Sprintf("ri.ontology.%s.%s.%s", ontology, objectType, primaryKey)
}

func parseRFC3339Param(v string) (time.Time, error) {
	if v == "" {
		return time.Time{}, errors.New("missing required parameter")
	}
	return time.Parse(time.RFC3339, v)
}

func parseAggParam(v string) (timeseries.Agg, error) {
	if v == "" {
		return timeseries.AggAvg, nil
	}
	switch strings.ToUpper(v) {
	case string(timeseries.AggAvg):
		return timeseries.AggAvg, nil
	case string(timeseries.AggMin):
		return timeseries.AggMin, nil
	case string(timeseries.AggMax):
		return timeseries.AggMax, nil
	case string(timeseries.AggSum):
		return timeseries.AggSum, nil
	case string(timeseries.AggLast):
		return timeseries.AggLast, nil
	default:
		return "", fmt.Errorf("unsupported agg %q", v)
	}
}

func parseBucketParam(v string) (time.Duration, error) {
	if v == "" {
		return 0, errors.New("missing required parameter")
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, errors.New("bucket must be positive")
	}
	return d, nil
}
