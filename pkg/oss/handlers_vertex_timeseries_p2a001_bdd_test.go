package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/timeseries"
)

type recordingVertexQuerier struct {
	calls int
}

func (q *recordingVertexQuerier) Query(_ context.Context, _ timeseries.VertexQuery) (*timeseries.VertexQueryResult, error) {
	q.calls++
	return &timeseries.VertexQueryResult{}, nil
}

func TestBDD_VertexTimeSeries_GivenNonForwardWindow_WhenGetThen400AndNoQuery(t *testing.T) {
	from := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		to   time.Time
	}{
		{name: "zero length", to: from},
		{name: "inverted", to: from.Add(-time.Minute)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := &recordingVertexQuerier{}
			router := newVertexTSRouter(q)

			url := "/api/v2/ontologies/ri.ontology.main.ontology.vtx/objects/Airport/JFK/timeseries/passengerThroughput?from=" +
				from.Format(time.RFC3339) + "&to=" + tt.to.Format(time.RFC3339) + "&agg=AVG&bucket=5m"
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			var body map[string]interface{}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode body: %v; body=%s", err, w.Body.String())
			}
			if body["errorName"] != "InvalidTimeWindow" {
				t.Fatalf("errorName = %v, want InvalidTimeWindow; body=%s", body["errorName"], w.Body.String())
			}
			if q.calls != 0 {
				t.Fatalf("querier calls = %d, want 0", q.calls)
			}
		})
	}
}
