package oss_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/timeseries"
)

// downsamplerSpy is a Store + Downsampler test double. It tracks
// whether the handler took the StreamPoints path (legacy) or the
// DownsamplePoints path (US-435 pushdown).
type downsamplerSpy struct {
	streamCalls     int
	downsampleCalls int
	lastSpec        timeseries.DownsampleSpec
	pointsToReturn  []timeseries.Point
}

func (s *downsamplerSpy) FirstPoint(ctx context.Context, key timeseries.SeriesKey) (*timeseries.Point, error) {
	if len(s.pointsToReturn) == 0 {
		return nil, timeseries.ErrNoPoints
	}
	p := s.pointsToReturn[0]
	return &p, nil
}

func (s *downsamplerSpy) LastPoint(ctx context.Context, key timeseries.SeriesKey) (*timeseries.Point, error) {
	if len(s.pointsToReturn) == 0 {
		return nil, timeseries.ErrNoPoints
	}
	p := s.pointsToReturn[len(s.pointsToReturn)-1]
	return &p, nil
}

func (s *downsamplerSpy) StreamPoints(ctx context.Context, key timeseries.SeriesKey) ([]timeseries.Point, error) {
	s.streamCalls++
	out := make([]timeseries.Point, len(s.pointsToReturn))
	copy(out, s.pointsToReturn)
	return out, nil
}

func (s *downsamplerSpy) AppendPoint(ctx context.Context, key timeseries.SeriesKey, p timeseries.Point) error {
	s.pointsToReturn = append(s.pointsToReturn, p)
	return nil
}

func (s *downsamplerSpy) DownsamplePoints(ctx context.Context, key timeseries.SeriesKey, spec timeseries.DownsampleSpec) ([]timeseries.Point, error) {
	s.downsampleCalls++
	s.lastSpec = spec
	// Synthesise a single bucket so the response structure is exercised.
	return []timeseries.Point{
		{Time: time.Unix(0, 0).UTC(), Value: 42.0},
	}, nil
}

// setupTimeSeriesTestWithStore is the parameterised cousin of
// setupTimeSeriesTest — same wiring, caller-supplied store.
func setupTimeSeriesTestWithStore(t *testing.T, store timeseries.Store) http.Handler {
	t.Helper()
	svc, mgr, repo, _ := setupOSSTest(t)
	repo.addObjectType(&oms.ObjectType{
		RID:         "ri.ontology.main.object-type.sensor",
		OntologyRID: testOntologyRID,
		APIName:     "sensor",
		DisplayName: "Sensor",
		PrimaryKey:  "sensorId",
		Status:      "ACTIVE",
		Visibility:  "NORMAL",
	})
	if _, err := mgr.EnsureIndex("sensor", []index.Property{
		{APIName: "sensorId", BaseType: "string", IsSearchable: true},
		{APIName: "temperature", BaseType: "timeseries", IsSearchable: false},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("sensor", "s1", map[string]interface{}{
		"sensorId":    "s1",
		"temperature": "ri.timeseries.main.series.s1-temperature",
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	h := oss.NewHandler(svc)
	h.SetTimeSeriesStore(store)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

// TestTransform_PushdownFiresForResampleOnDownsampler proves the handler
// dispatches a single resample step over a Downsampler-implementing
// store via DownsamplePoints — bypassing StreamPoints entirely, which
// is the entire point of US-435 (constant-time-on-the-wire query for
// large series).
func TestTransform_PushdownFiresForResampleOnDownsampler(t *testing.T) {
	spy := &downsamplerSpy{}
	r := setupTimeSeriesTestWithStore(t, spy)

	body := `{
	  "source": {"objectType":"sensor","primaryKey":"s1","property":"temperature"},
	  "transforms": [{"op":"resample","params":{"interval":"5m","agg":"avg"}}]
	}`
	req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if spy.downsampleCalls != 1 {
		t.Errorf("downsampleCalls = %d, want 1 (handler should push down)", spy.downsampleCalls)
	}
	if spy.streamCalls != 0 {
		t.Errorf("streamCalls = %d, want 0 (pushdown means no stream fetch)", spy.streamCalls)
	}
	if spy.lastSpec.Step != 5*time.Minute {
		t.Errorf("spec.Step = %v, want 5m", spy.lastSpec.Step)
	}
	if spy.lastSpec.Aggregation != timeseries.DownsampleAvg {
		t.Errorf("spec.Aggregation = %v, want avg", spy.lastSpec.Aggregation)
	}

	resp := decodeTransform(t, rec.Body.Bytes())
	if len(resp.Points) != 1 {
		t.Fatalf("len = %d, want 1", len(resp.Points))
	}
	if resp.Points[0].Value.(float64) != 42.0 {
		t.Errorf("value = %v, want 42 (the spy's synthesised bucket)", resp.Points[0].Value)
	}
}

// TestTransform_NoPushdownWhenChainHasMultipleSteps verifies the
// pushdown is gated on a single-step chain. Anything more than one
// step requires the local ApplyChain to compose, so the handler MUST
// fall back to StreamPoints.
func TestTransform_NoPushdownWhenChainHasMultipleSteps(t *testing.T) {
	spy := &downsamplerSpy{
		pointsToReturn: []timeseries.Point{
			{Time: time.Unix(0, 0).UTC(), Value: 1.0},
			{Time: time.Unix(60, 0).UTC(), Value: 2.0},
			{Time: time.Unix(120, 0).UTC(), Value: 3.0},
		},
	}
	r := setupTimeSeriesTestWithStore(t, spy)

	body := `{
	  "source": {"objectType":"sensor","primaryKey":"s1","property":"temperature"},
	  "transforms": [
	    {"op":"resample","params":{"interval":"5m","agg":"avg"}},
	    {"op":"scale","params":{"factor":2}}
	  ]
	}`
	req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if spy.downsampleCalls != 0 {
		t.Errorf("downsampleCalls = %d, want 0 (multi-step chain should NOT push down)", spy.downsampleCalls)
	}
	if spy.streamCalls != 1 {
		t.Errorf("streamCalls = %d, want 1 (multi-step chain falls back to stream)", spy.streamCalls)
	}
}

// TestTransform_NoPushdownForNonResampleSingleStep verifies the
// pushdown only fires for `resample` — not `scale`, `diff`, etc.
func TestTransform_NoPushdownForNonResampleSingleStep(t *testing.T) {
	spy := &downsamplerSpy{
		pointsToReturn: []timeseries.Point{
			{Time: time.Unix(0, 0).UTC(), Value: 5.0},
		},
	}
	r := setupTimeSeriesTestWithStore(t, spy)

	body := `{
	  "source": {"objectType":"sensor","primaryKey":"s1","property":"temperature"},
	  "transforms": [{"op":"scale","params":{"factor":3}}]
	}`
	req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if spy.downsampleCalls != 0 {
		t.Errorf("downsampleCalls = %d, want 0 (scale is not resample)", spy.downsampleCalls)
	}
	if spy.streamCalls != 1 {
		t.Errorf("streamCalls = %d, want 1", spy.streamCalls)
	}
}

// TestTransform_ResampleAggsPushDown verifies every aggregation in the
// resample taxonomy translates to a DownsampleSpec.Aggregation that
// the spy can recognise.
func TestTransform_ResampleAggsPushDown(t *testing.T) {
	cases := []struct {
		agg  string
		want timeseries.DownsampleAggregation
	}{
		{"avg", timeseries.DownsampleAvg},
		{"mean", timeseries.DownsampleAvg},
		{"sum", timeseries.DownsampleSum},
		{"min", timeseries.DownsampleMin},
		{"max", timeseries.DownsampleMax},
		{"count", timeseries.DownsampleCount},
	}
	for _, tc := range cases {
		t.Run(tc.agg, func(t *testing.T) {
			spy := &downsamplerSpy{}
			r := setupTimeSeriesTestWithStore(t, spy)
			body, _ := json.Marshal(map[string]interface{}{
				"source": map[string]string{"objectType": "sensor", "primaryKey": "s1", "property": "temperature"},
				"transforms": []map[string]interface{}{
					{"op": "resample", "params": map[string]interface{}{"interval": "1h", "agg": tc.agg}},
				},
			})
			req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewReader(body))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			if spy.downsampleCalls != 1 {
				t.Fatalf("downsampleCalls = %d, want 1", spy.downsampleCalls)
			}
			if spy.lastSpec.Aggregation != tc.want {
				t.Errorf("spec.Aggregation = %v, want %v", spy.lastSpec.Aggregation, tc.want)
			}
			if spy.lastSpec.Step != time.Hour {
				t.Errorf("spec.Step = %v, want 1h", spy.lastSpec.Step)
			}
		})
	}
}
