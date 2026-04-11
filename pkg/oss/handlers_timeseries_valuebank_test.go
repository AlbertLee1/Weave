package oss_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/timeseries"
)

// US-038: TimeSeriesValueBankProperty endpoints
//
// Foundry exposes two value-bank endpoints in addition to the three
// TimeSeriesProperty endpoints from US-037:
//
//   GET  .../timeseries/{propertyName}/latestValue     (note: {propertyName})
//   POST .../timeseries/{property}/streamValues        (note: {property})
//
// The path parameter inconsistency ({propertyName} vs {property}) matches
// Foundry's OpenAPI exactly and must be preserved.
//
// Backed by the same timeseries.Store as TimeSeriesProperty: latestValue
// returns the latest point, streamValues returns all points. Wire shape is
// TimeSeriesPoint {time, value}, identical to US-037.

func TestGetTimeSeriesLatestValue(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/latestValue",
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

func TestGetTimeSeriesLatestValue_EmptySeries(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	// Point the object at a series that has no appended points.
	req := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/humidity/latestValue",
		nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostTimeSeriesStreamValues(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/streamValues",
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
		t.Fatalf("got %d values, want 3", len(out))
	}
	if !out[0].Time.Equal(mustParseTime(t, "2026-04-01T00:00:00Z")) {
		t.Errorf("out[0].Time = %v", out[0].Time)
	}
	if !out[2].Time.Equal(mustParseTime(t, "2026-04-03T00:00:00Z")) {
		t.Errorf("out[2].Time = %v", out[2].Time)
	}
}

func TestPostTimeSeriesStreamValues_ArrowNotSupported(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/streamValues?format=ARROW",
		bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

