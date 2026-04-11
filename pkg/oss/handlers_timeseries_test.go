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

// setupTimeSeriesTest wires an OSS handler with a MemoryStore, creates a
// sensor object, and seeds three temperature readings.
func setupTimeSeriesTest(t *testing.T) (http.Handler, timeseries.Store) {
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

	store := timeseries.NewMemoryStore()
	ctx := context.Background()
	key := timeseries.SeriesKey{
		Ontology:   testOntologyRID,
		ObjectType: "sensor",
		PrimaryKey: "s1",
		Property:   "temperature",
	}
	points := []timeseries.Point{
		{Time: mustParseTime(t, "2026-04-01T00:00:00Z"), Value: 21.0},
		{Time: mustParseTime(t, "2026-04-02T00:00:00Z"), Value: 22.5},
		{Time: mustParseTime(t, "2026-04-03T00:00:00Z"), Value: 23.5},
	}
	for _, p := range points {
		if err := store.AppendPoint(ctx, key, p); err != nil {
			t.Fatalf("AppendPoint: %v", err)
		}
	}

	h := oss.NewHandler(svc)
	h.SetTimeSeriesStore(store)
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r, store
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tt
}

func TestGetTimeSeriesFirstPoint(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/firstPoint",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out timeseries.Point
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := mustParseTime(t, "2026-04-01T00:00:00Z")
	if !out.Time.Equal(want) {
		t.Errorf("time = %v, want %v", out.Time, want)
	}
	if out.Value != 21.0 {
		t.Errorf("value = %v, want 21.0", out.Value)
	}
}

func TestGetTimeSeriesLastPoint(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/lastPoint",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out timeseries.Point
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := mustParseTime(t, "2026-04-03T00:00:00Z")
	if !out.Time.Equal(want) {
		t.Errorf("time = %v, want %v", out.Time, want)
	}
	if out.Value != 23.5 {
		t.Errorf("value = %v, want 23.5", out.Value)
	}
}

func TestGetTimeSeriesStreamPoints(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/streamPoints",
		bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var out []timeseries.Point
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("got %d points, want 3", len(out))
	}
	if !out[0].Time.Equal(mustParseTime(t, "2026-04-01T00:00:00Z")) {
		t.Errorf("out[0].Time = %v", out[0].Time)
	}
	if !out[2].Time.Equal(mustParseTime(t, "2026-04-03T00:00:00Z")) {
		t.Errorf("out[2].Time = %v", out[2].Time)
	}
}

func TestGetTimeSeriesStreamPoints_ArrowNotSupported(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/streamPoints?format=ARROW",
		bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// ARROW format is accepted as a query parameter but we only emit JSON
	// (single-machine scope); the server must respond with 400
	// UnsupportedFormat rather than silently returning JSON for an ARROW
	// request.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetTimeSeriesFirstPoint_EmptySeries(t *testing.T) {
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
		{APIName: "sensorId", BaseType: "string"},
		{APIName: "temperature", BaseType: "timeseries"},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("sensor", "s1", map[string]interface{}{
		"sensorId":    "s1",
		"temperature": "ri.timeseries.main.series.empty",
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	h := oss.NewHandler(svc)
	h.SetTimeSeriesStore(timeseries.NewMemoryStore())
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/firstPoint",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetTimeSeriesFirstPoint_NotConfigured(t *testing.T) {
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
		{APIName: "sensorId", BaseType: "string"},
	}); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}
	if err := mgr.IndexDocument("sensor", "s1", map[string]interface{}{
		"sensorId": "s1",
	}); err != nil {
		t.Fatalf("IndexDocument: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// No SetTimeSeriesStore → store is nil.
	h := oss.NewHandler(svc)
	r := chi.NewRouter()
	h.RegisterRoutes(r)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/firstPoint",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body=%s", rec.Code, rec.Body.String())
	}
}
