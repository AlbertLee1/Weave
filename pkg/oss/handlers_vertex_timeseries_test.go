package oss_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/timeseries"
)

// VTX-030: REST handler GET /api/v2/.../objects/{objectType}/{primaryKey}/timeseries/{property}.

// fakeVertexQuerier captures the last VertexQuery and returns canned points.
type fakeVertexQuerier struct {
	last       timeseries.VertexQuery
	points     []timeseries.BucketedPoint
	warning    string
	lastObs    *time.Time
	err        error
}

func (f *fakeVertexQuerier) Query(_ context.Context, q timeseries.VertexQuery) (*timeseries.VertexQueryResult, error) {
	f.last = q
	if f.err != nil {
		return nil, f.err
	}
	return &timeseries.VertexQueryResult{
		Points:         f.points,
		Warning:        f.warning,
		LastObservedAt: f.lastObs,
	}, nil
}

func newVertexTSRouter(querier oss.VertexTimeSeriesQuerier) http.Handler {
	r := chi.NewRouter()
	h := oss.NewHandler(panicService{})
	h.SetVertexTimeSeriesQuerier(querier)
	h.RegisterRoutes(r)
	return r
}

// panicService is a Service whose only purpose is to satisfy NewHandler. The
// vertex timeseries handler does NOT call svc.GetObject, so every method
// panics if invoked.
type panicService struct{}

func (panicService) GetObject(_ context.Context, _ oss.GetObjectRequest) (*oss.WireObject, error) {
	panic("not implemented")
}
func (panicService) ListObjects(_ context.Context, _ oss.ListObjectsRequest) (*oss.ObjectPage, error) {
	panic("not implemented")
}
func (panicService) SearchObjects(_ context.Context, _ oss.SearchObjectsRequest) (*oss.ObjectPage, error) {
	panic("not implemented")
}
func (panicService) CountObjects(_ context.Context, _ oss.CountObjectsRequest) (*oss.CountObjectsResponse, error) {
	panic("not implemented")
}
func (panicService) ListLinkedObjects(_ context.Context, _ oss.LinkedObjectsRequest) (*oss.ObjectPage, error) {
	panic("not implemented")
}
func (panicService) GetLinkedObject(_ context.Context, _ oss.GetLinkedObjectRequest) (*oss.WireObject, error) {
	panic("not implemented")
}

// TestVertexTimeSeriesHandler_Given_QueryParams_When_Get_Then_ReturnsBucketSeries
// covers VTX-030 BDD #1.
func TestVertexTimeSeriesHandler_Given_QueryParams_When_Get_Then_ReturnsBucketSeries(t *testing.T) {
	from := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	to := from.Add(15 * time.Minute)
	q := &fakeVertexQuerier{
		points: []timeseries.BucketedPoint{
			{Time: from, Value: 100},
			{Time: from.Add(5 * time.Minute), Value: 110},
			{Time: from.Add(10 * time.Minute), Value: 120},
		},
	}
	router := newVertexTSRouter(q)

	url := "/api/v2/ontologies/ri.ontology.main.ontology.vtx/objects/Airport/JFK/timeseries/passengerThroughput?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&agg=AVG&bucket=5m"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp timeseries.VertexQueryResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	if len(resp.Points) != 3 {
		t.Fatalf("expected 3 points, got %d", len(resp.Points))
	}
	if resp.Points[0].Value != 100 {
		t.Errorf("points[0].value = %v, want 100", resp.Points[0].Value)
	}

	// Verify the handler forwarded query params correctly.
	if q.last.Agg != timeseries.AggAvg {
		t.Errorf("agg = %q, want AVG", q.last.Agg)
	}
	if q.last.Bucket != 5*time.Minute {
		t.Errorf("bucket = %v, want 5m", q.last.Bucket)
	}
	if !q.last.From.Equal(from) {
		t.Errorf("from = %v, want %v", q.last.From, from)
	}
	if q.last.Property != "passengerThroughput" {
		t.Errorf("property = %q", q.last.Property)
	}
	if q.last.ScenarioID != "" {
		t.Errorf("scenarioId = %q, want empty", q.last.ScenarioID)
	}
}

// TestVertexTimeSeriesHandler_Given_ScenarioParam_When_Get_Then_ForwardsScenarioID
// covers VTX-030 BDD #2 (scenarioId query param flows into querier).
func TestVertexTimeSeriesHandler_Given_ScenarioParam_When_Get_Then_ForwardsScenarioID(t *testing.T) {
	from := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	to := from.Add(5 * time.Minute)
	override := 999.0
	q := &fakeVertexQuerier{
		points: []timeseries.BucketedPoint{{Time: from, Value: override}},
	}
	router := newVertexTSRouter(q)

	url := "/api/v2/ontologies/ri.ontology.main.ontology.vtx/objects/Airport/JFK/timeseries/passengerThroughput?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339) + "&agg=AVG&bucket=5m&scenarioId=ri.vertex.main.scenario.s1"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	}
	if q.last.ScenarioID != "ri.vertex.main.scenario.s1" {
		t.Errorf("scenarioId not forwarded: got %q", q.last.ScenarioID)
	}
	var resp timeseries.VertexQueryResult
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Points[0].Value != override {
		t.Errorf("override not visible to client: got %v", resp.Points[0].Value)
	}
}

// TestVertexTimeSeriesHandler_Given_MissingFromParam_When_Get_Then_400
// covers param validation (the BDD doesn't enumerate every 400 case but the
// handler must not pass garbage durations to the querier).
func TestVertexTimeSeriesHandler_Given_MissingFromParam_When_Get_Then_400(t *testing.T) {
	q := &fakeVertexQuerier{}
	router := newVertexTSRouter(q)

	url := "/api/v2/ontologies/ri.ontology.main.ontology.vtx/objects/Airport/JFK/timeseries/passengerThroughput?to=2026-05-15T00:05:00Z&agg=AVG&bucket=5m"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestVertexTimeSeriesHandler_Given_BadBucket_When_Get_Then_400
// covers param validation for bucket.
func TestVertexTimeSeriesHandler_Given_BadBucket_When_Get_Then_400(t *testing.T) {
	q := &fakeVertexQuerier{}
	router := newVertexTSRouter(q)

	url := "/api/v2/ontologies/ri.ontology.main.ontology.vtx/objects/Airport/JFK/timeseries/passengerThroughput?from=2026-05-15T00:00:00Z&to=2026-05-15T00:05:00Z&agg=AVG&bucket=banana"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", w.Code, w.Body.String())
	}
}

// TestVertexTimeSeriesHandler_Given_NoQuerier_When_Get_Then_503Configured
// covers SetVertexTimeSeriesQuerier-not-called path.
func TestVertexTimeSeriesHandler_Given_NoQuerier_When_Get_Then_503Configured(t *testing.T) {
	r := chi.NewRouter()
	h := oss.NewHandler(panicService{})
	h.RegisterRoutes(r)

	url := "/api/v2/ontologies/ri.ontology.main.ontology.vtx/objects/Airport/JFK/timeseries/passengerThroughput?from=2026-05-15T00:00:00Z&to=2026-05-15T00:05:00Z&agg=AVG&bucket=5m"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (querier not configured); body=%s", w.Code, w.Body.String())
	}
}
