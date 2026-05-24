package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/timeseries"
)

// TestBDD_OSS_RejectsAmbiguousJSONBody continues the P2A-30x
// ambiguous-JSON hardening series (rounds 1, 15, 16) into pkg/oss.
// Three POST endpoints on the OSv2 core query / timeseries surface
// still decoded via `json.NewDecoder(r.Body).Decode(&req)` which
// accepts only the first JSON value and silently drops trailing
// bytes:
//
//   - POST /api/v2/ontologies/{o}/interfaces/{iface}/aggregate
//     (InterfaceAggregateObjects)
//   - POST /api/v2/ontologies/{o}/objects/{ot}/{pk}/timeseries/{prop}/points
//     (AppendTimeSeriesPoint) — write surface, audit trail must
//     reflect exactly the point that landed
//   - POST /api/v2/ontologies/{o}/timeseries/transform
//     (TransformTimeSeries)
//
// AggregateObjects (the per-objectType variant) requires the
// aggregation engine in the test harness; setupOSSTest does not
// wire it. That site has the same one-line fix and ships in the
// commit anyway — a future round will add an aggregation-engine
// harness to exercise the BDD path.
//
// Smuggling vector on these endpoints:
//   - Aggregate: a body with two concatenated aggregation specs
//     returns the first while an audit pipeline re-parsing the
//     raw bytes sees the trailing definition as if the operator
//     asked for a different metric.
//   - AppendPoint: an attacker who can influence the body smuggles
//     a value at a different timestamp that audit pipelines
//     misattribute to a different ingest event.
//
// Fix mirrors rounds 15+16: swap to httputil.ReadJSON which
// enforces dec.Decode(&extra) == io.EOF and returns the
// "single JSON value" rejection.
func TestBDD_OSS_RejectsAmbiguousJSONBody(t *testing.T) {
	t.Run("InterfaceAggregateObjects rejects concatenated JSON", func(t *testing.T) {
		_, r, _ := setupInterfaceTest(t)

		body := `{"aggregation":[{"type":"count","name":"a"}]}{"aggregation":[{"type":"count","name":"b"}]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/worker/aggregate",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assertOSSAggregateSingleJSONRejection(t, rec, "InvalidAggregationRequest")
	})

	t.Run("InterfaceAggregateObjects accepts well-formed body (regression guard)", func(t *testing.T) {
		_, r, _ := setupInterfaceTest(t)
		body := `{"aggregation":[{"type":"count","name":"total"}]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+testIfaceOntologyRID+"/interfaces/worker/aggregate",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("happy interface aggregate: status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("AppendTimeSeriesPoint rejects concatenated JSON without persisting either point", func(t *testing.T) {
		r, store := setupTimeSeriesTest(t)
		baselineCount := timeSeriesPointCount(t, store)

		body := `{"time":"2026-05-01T00:00:00Z","value":99.9}{"time":"2026-05-02T00:00:00Z","value":42.0}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/points",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assertOSSAggregateSingleJSONRejection(t, rec, "TimeSeriesPointInvalidBody")

		afterCount := timeSeriesPointCount(t, store)
		if afterCount != baselineCount {
			t.Errorf("ambiguous body must not append any timeseries point: baseline=%d after=%d",
				baselineCount, afterCount)
		}
	})

	t.Run("TransformTimeSeries rejects concatenated JSON", func(t *testing.T) {
		r, _ := setupTimeSeriesTest(t)
		body := `{"points":[{"time":"2026-05-01T00:00:00Z","value":1.0}],"transforms":[{"type":"resample","step":"PT1H","agg":"avg"}]}{"transforms":[]}`
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+testOntologyRID+"/timeseries/transform",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assertOSSAggregateSingleJSONRejection(t, rec, "TimeSeriesTransformInvalidBody")
	})
}

func assertOSSAggregateSingleJSONRejection(t *testing.T, rec *httptest.ResponseRecorder, wantErrorName string) {
	t.Helper()
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var env struct {
		ErrorName  string            `json:"errorName"`
		Parameters map[string]string `json:"parameters"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&env)
	if env.ErrorName != wantErrorName {
		t.Errorf("errorName: got %q, want %q", env.ErrorName, wantErrorName)
	}
	if !strings.Contains(strings.ToLower(env.Parameters["reason"]), "single json value") {
		t.Errorf("reason should mention 'single JSON value', got %q", env.Parameters["reason"])
	}
}

// timeSeriesPointCount queries the in-memory store for the seeded
// sensor / s1 / temperature series and returns the current point
// count. Used as a non-mutation snapshot for the
// AppendTimeSeriesPoint rejection assertion.
func timeSeriesPointCount(t *testing.T, store timeseries.Store) int {
	t.Helper()
	pts, err := store.StreamPoints(context.Background(), timeseries.SeriesKey{
		Ontology:   testOntologyRID,
		ObjectType: "sensor",
		PrimaryKey: "s1",
		Property:   "temperature",
	})
	if err != nil {
		t.Fatalf("StreamPoints: %v", err)
	}
	return len(pts)
}
