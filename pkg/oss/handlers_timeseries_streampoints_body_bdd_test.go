package oss_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/liyang/weave/pkg/timeseries"
)

// These BDD scenarios cover Foundry parity for the streamPoints request
// body (StreamTimeSeriesPointsRequest): the handler must honor the
// optional `range` (absolute + relative) and `aggregate` (periodic
// downsample) fields, stay backward compatible for an empty body, and
// reject malformed bodies with a 400.

func streamPointsPath(property string) string {
	return "/api/v2/ontologies/" + testOntologyRID + "/objects/sensor/s1/timeseries/" + property + "/streamPoints"
}

func postStreamPoints(t *testing.T, r http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body == "" {
		rdr = bytes.NewReader(nil)
	} else {
		rdr = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(http.MethodPost, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func decodePoints(t *testing.T, b []byte) []timeseries.Point {
	t.Helper()
	var out []timeseries.Point
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal points: %v (body=%s)", err, string(b))
	}
	return out
}

// storeWithPoints wires a handler over a MemoryStore seeded with the given
// points on the sensor/s1/temperature series.
func storeWithPoints(t *testing.T, points []timeseries.Point) http.Handler {
	t.Helper()
	store := timeseries.NewMemoryStore()
	key := timeseries.SeriesKey{
		Ontology:   testOntologyRID,
		ObjectType: "sensor",
		PrimaryKey: "s1",
		Property:   "temperature",
	}
	for _, p := range points {
		if err := store.AppendPoint(context.Background(), key, p); err != nil {
			t.Fatalf("AppendPoint: %v", err)
		}
	}
	return setupTimeSeriesTestWithStore(t, store)
}

// Given a series with daily readings, When a client posts an absolute range
// [04-02, 04-03), Then only the 04-02 point is returned (start inclusive,
// end exclusive) — proving the body is honored rather than ignored.
func TestBDD_StreamPoints_AbsoluteRangeFiltersWindow(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)
	body := `{"range":{"type":"absolute","startTime":"2026-04-02T00:00:00Z","endTime":"2026-04-03T00:00:00Z"}}`
	rec := postStreamPoints(t, r, streamPointsPath("temperature"), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	out := decodePoints(t, rec.Body.Bytes())
	if len(out) != 1 {
		t.Fatalf("got %d points, want 1 (end exclusive): %+v", len(out), out)
	}
	if !out[0].Time.Equal(mustParseTime(t, "2026-04-02T00:00:00Z")) {
		t.Errorf("point time = %v, want 2026-04-02", out[0].Time)
	}
}

// Given a series with daily readings, When a client posts an absolute range
// with only startTime, Then all points at or after start are returned.
func TestBDD_StreamPoints_AbsoluteRangeStartOnly(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)
	body := `{"range":{"type":"absolute","startTime":"2026-04-02T00:00:00Z"}}`
	rec := postStreamPoints(t, r, streamPointsPath("temperature"), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	out := decodePoints(t, rec.Body.Bytes())
	if len(out) != 2 {
		t.Fatalf("got %d points, want 2 (04-02, 04-03): %+v", len(out), out)
	}
}

// Given a series with points at now-3h/-1h/-30m/-1m, When a client posts a
// relative range "2 HOURS BEFORE", Then only points within the last two
// hours are returned. A tolerant boundary (points far from the edge) keeps
// this deterministic.
func TestBDD_StreamPoints_RelativeRangeFiltersWindow(t *testing.T) {
	now := time.Now().UTC()
	r := storeWithPoints(t, []timeseries.Point{
		{Time: now.Add(-3 * time.Hour), Value: 1.0},
		{Time: now.Add(-1 * time.Hour), Value: 2.0},
		{Time: now.Add(-30 * time.Minute), Value: 3.0},
		{Time: now.Add(-1 * time.Minute), Value: 4.0},
	})
	body := `{"range":{"type":"relative","startTime":{"when":"BEFORE","value":2,"unit":"HOURS"}}}`
	rec := postStreamPoints(t, r, streamPointsPath("temperature"), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	out := decodePoints(t, rec.Body.Bytes())
	if len(out) != 3 {
		t.Fatalf("got %d points, want 3 (last 2h): %+v", len(out), out)
	}
	cutoff := now.Add(-2 * time.Hour)
	for _, p := range out {
		if p.Time.Before(cutoff) {
			t.Errorf("point %v is older than the 2h cutoff %v", p.Time, cutoff)
		}
	}
}

// Given three hourly readings on the same UTC day, When a client posts a
// periodic MEAN aggregate with a 1-day window, Then a single downsampled
// bucket is returned with the daily mean.
func TestBDD_StreamPoints_AggregateDownsamples(t *testing.T) {
	r := storeWithPoints(t, []timeseries.Point{
		{Time: mustParseTime(t, "2026-04-01T00:00:00Z"), Value: 10.0},
		{Time: mustParseTime(t, "2026-04-01T01:00:00Z"), Value: 20.0},
		{Time: mustParseTime(t, "2026-04-01T02:00:00Z"), Value: 30.0},
	})
	body := `{"aggregate":{"method":"MEAN","strategy":{"type":"periodic","windowSize":{"value":1,"unit":"DAYS","type":"duration"}}}}`
	rec := postStreamPoints(t, r, streamPointsPath("temperature"), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	out := decodePoints(t, rec.Body.Bytes())
	if len(out) != 1 {
		t.Fatalf("got %d buckets, want 1: %+v", len(out), out)
	}
	if v, _ := out[0].Value.(float64); v != 20.0 {
		t.Errorf("bucket mean = %v, want 20", out[0].Value)
	}
	if !out[0].Time.Equal(mustParseTime(t, "2026-04-01T00:00:00Z")) {
		t.Errorf("bucket time = %v, want day start", out[0].Time)
	}
}

// Given three hourly readings, When a client posts a range that excludes
// the last reading AND a SUM aggregate, Then range filtering is applied
// before the bucket reduce.
func TestBDD_StreamPoints_AggregateWithRange(t *testing.T) {
	r := storeWithPoints(t, []timeseries.Point{
		{Time: mustParseTime(t, "2026-04-01T00:00:00Z"), Value: 10.0},
		{Time: mustParseTime(t, "2026-04-01T01:00:00Z"), Value: 20.0},
		{Time: mustParseTime(t, "2026-04-01T02:00:00Z"), Value: 30.0},
	})
	body := `{"range":{"type":"absolute","startTime":"2026-04-01T00:00:00Z","endTime":"2026-04-01T02:00:00Z"},` +
		`"aggregate":{"method":"SUM","strategy":{"type":"periodic","windowSize":{"value":1,"unit":"DAYS","type":"duration"}}}}`
	rec := postStreamPoints(t, r, streamPointsPath("temperature"), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	out := decodePoints(t, rec.Body.Bytes())
	if len(out) != 1 {
		t.Fatalf("got %d buckets, want 1: %+v", len(out), out)
	}
	if v, _ := out[0].Value.(float64); v != 30.0 {
		t.Errorf("bucket sum = %v, want 30 (10+20, 02:00 excluded)", out[0].Value)
	}
}

// Given a series, When a client posts an empty JSON body OR no body at all,
// Then the full series is returned (backward compatible with pre-body
// clients).
func TestBDD_StreamPoints_EmptyBodyReturnsAll(t *testing.T) {
	for _, body := range []string{`{}`, ``} {
		r, _ := setupTimeSeriesTest(t)
		rec := postStreamPoints(t, r, streamPointsPath("temperature"), body)
		if rec.Code != http.StatusOK {
			t.Fatalf("body=%q status = %d, resp=%s", body, rec.Code, rec.Body.String())
		}
		out := decodePoints(t, rec.Body.Bytes())
		if len(out) != 3 {
			t.Fatalf("body=%q got %d points, want 3 (full series)", body, len(out))
		}
	}
}

// Given a series, When a client posts a malformed range, Then the server
// responds 400 with an errorName (not a 500 or a silent full stream).
func TestBDD_StreamPoints_InvalidRangeReturns400(t *testing.T) {
	cases := map[string]string{
		"bad-absolute-time":  `{"range":{"type":"absolute","startTime":"not-a-time"}}`,
		"bad-relative-unit":  `{"range":{"type":"relative","startTime":{"when":"BEFORE","value":5,"unit":"FORTNIGHTS"}}}`,
		"bad-relative-when":  `{"range":{"type":"relative","startTime":{"when":"SIDEWAYS","value":5,"unit":"HOURS"}}}`,
		"missing-range-type": `{"range":{"startTime":"2026-04-01T00:00:00Z"}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r, _ := setupTimeSeriesTest(t)
			rec := postStreamPoints(t, r, streamPointsPath("temperature"), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
			}
			var e struct {
				ErrorName string `json:"errorName"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || e.ErrorName == "" {
				t.Errorf("want errorName in body, got %s (err=%v)", rec.Body.String(), err)
			}
		})
	}
}

// Given a series, When a client posts an unsupported aggregate (rolling
// strategy or a statistical method with no bucket-reduce equivalent), Then
// the server responds 400.
func TestBDD_StreamPoints_InvalidAggregateReturns400(t *testing.T) {
	cases := map[string]string{
		"rolling-strategy":   `{"aggregate":{"method":"MEAN","strategy":{"type":"rolling","windowSize":{"type":"pointsCount","count":5}}}}`,
		"cumulative":         `{"aggregate":{"method":"MEAN","strategy":{"type":"cumulative"}}}`,
		"unsupported-method": `{"aggregate":{"method":"STANDARD_DEVIATION","strategy":{"type":"periodic","windowSize":{"value":1,"unit":"DAYS","type":"duration"}}}}`,
		"missing-window":     `{"aggregate":{"method":"MEAN","strategy":{"type":"periodic"}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			r, _ := setupTimeSeriesTest(t)
			rec := postStreamPoints(t, r, streamPointsPath("temperature"), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

// Given a store that implements Downsampler, When a client posts an
// aggregate with a range, Then the handler pushes the reduce down to the
// store (DownsamplePoints), never streaming raw points, and passes the
// resolved window + step + aggregation in the spec.
func TestBDD_StreamPoints_AggregatePushesDownToDownsampler(t *testing.T) {
	spy := &downsamplerSpy{}
	r := setupTimeSeriesTestWithStore(t, spy)
	body := `{"range":{"type":"absolute","startTime":"2026-04-01T00:00:00Z","endTime":"2026-04-02T00:00:00Z"},` +
		`"aggregate":{"method":"MEAN","strategy":{"type":"periodic","windowSize":{"value":5,"unit":"MINUTES","type":"duration"}}}}`
	rec := postStreamPoints(t, r, streamPointsPath("temperature"), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if spy.downsampleCalls != 1 {
		t.Errorf("downsampleCalls = %d, want 1 (should push down)", spy.downsampleCalls)
	}
	if spy.streamCalls != 0 {
		t.Errorf("streamCalls = %d, want 0 (pushdown means no raw stream)", spy.streamCalls)
	}
	if spy.lastSpec.Step != 5*time.Minute {
		t.Errorf("spec.Step = %v, want 5m", spy.lastSpec.Step)
	}
	if spy.lastSpec.Aggregation != timeseries.DownsampleAvg {
		t.Errorf("spec.Aggregation = %v, want avg", spy.lastSpec.Aggregation)
	}
	if !spy.lastSpec.Start.Equal(mustParseTime(t, "2026-04-01T00:00:00Z")) {
		t.Errorf("spec.Start = %v, want 2026-04-01", spy.lastSpec.Start)
	}
	if !spy.lastSpec.End.Equal(mustParseTime(t, "2026-04-02T00:00:00Z")) {
		t.Errorf("spec.End = %v, want 2026-04-02", spy.lastSpec.End)
	}
}

// The value-bank streamValues endpoint shares the handler body, so it must
// honor range filtering too.
func TestBDD_StreamValues_AbsoluteRangeFiltersWindow(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)
	path := "/api/v2/ontologies/" + testOntologyRID + "/objects/sensor/s1/timeseries/temperature/streamValues"
	body := `{"range":{"type":"absolute","startTime":"2026-04-02T00:00:00Z","endTime":"2026-04-03T00:00:00Z"}}`
	rec := postStreamPoints(t, r, path, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	out := decodePoints(t, rec.Body.Bytes())
	if len(out) != 1 {
		t.Fatalf("got %d points, want 1 (end exclusive): %+v", len(out), out)
	}
}
