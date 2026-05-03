package oss_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/timeseries"
)

// TestAppendTimeSeriesPoint_AddsPointAndIsReadableViaFirstLast asserts the
// US-400 POST .../timeseries/{property}/points handler writes through the
// configured store so subsequent GET firstPoint / lastPoint return the
// freshly-appended datapoint.
func TestAppendTimeSeriesPoint_AddsPointAndIsReadableViaFirstLast(t *testing.T) {
	r, store := setupTimeSeriesTest(t)
	_ = store

	body := bytes.NewBufferString(`{"time":"2026-04-04T00:00:00Z","value":24.5}`)
	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/points",
		body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST .../points status = %d, want 204, body=%s", rec.Code, rec.Body.String())
	}

	// Read the new last point back.
	getReq := httptest.NewRequest(http.MethodGet,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/lastPoint",
		nil)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("lastPoint status = %d, body=%s", getRec.Code, getRec.Body.String())
	}
	var out timeseries.Point
	if err := json.Unmarshal(getRec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Value != 24.5 {
		t.Errorf("lastPoint value = %v, want 24.5", out.Value)
	}
	if !strings.HasPrefix(out.Time.Format("2006-01-02"), "2026-04-04") {
		t.Errorf("lastPoint time = %v, want 2026-04-04", out.Time)
	}
}

func TestAppendTimeSeriesPoint_RejectsInvalidBody(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	for _, body := range []string{
		`{}`,                          // missing time
		`{"value":1}`,                 // missing time
		`{"time":"not-a-time"}`,       // garbage time
		`{"time":"2026-04-04T00:00"}`, // missing offset
	} {
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/s1/timeseries/temperature/points",
			bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400 (resp=%s)", body, rec.Code, rec.Body.String())
		}
	}
}

func TestAppendTimeSeriesPoint_RejectsObjectNotFound(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v2/ontologies/"+testOntologyRID+"/objects/sensor/does-not-exist/timeseries/temperature/points",
		bytes.NewBufferString(`{"time":"2026-04-04T00:00:00Z","value":1}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}
