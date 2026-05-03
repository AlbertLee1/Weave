package oss_test

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/liyang/weave/pkg/timeseries"
)

const transformPath = "/api/v2/ontologies/" + testOntologyRID + "/timeseries/transform"

type transformResponse struct {
	Points []timeseries.Point `json:"points"`
}

func decodeTransform(t *testing.T, body []byte) transformResponse {
	t.Helper()
	var resp transformResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, string(body))
	}
	return resp
}

// TestTransformTimeSeries_InlinePoints_Diff exercises the inline-points
// branch with a simple diff transform and asserts the wire shape.
func TestTransformTimeSeries_InlinePoints_Diff(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	body := `{
	  "points": [
	    {"time":"2026-04-01T00:00:00Z","value":10},
	    {"time":"2026-04-01T00:01:00Z","value":15},
	    {"time":"2026-04-01T00:02:00Z","value":12}
	  ],
	  "transforms": [{"op":"diff"}]
	}`
	req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeTransform(t, rec.Body.Bytes())
	if len(resp.Points) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Points))
	}
	if v, ok := resp.Points[0].Value.(float64); !ok || v != 5.0 {
		t.Errorf("[0] value = %v, want 5", resp.Points[0].Value)
	}
	if v, ok := resp.Points[1].Value.(float64); !ok || v != -3.0 {
		t.Errorf("[1] value = %v, want -3", resp.Points[1].Value)
	}
}

// TestTransformTimeSeries_SourceFromStore exercises the source-resolved
// branch — the seed series is populated by setupTimeSeriesTest and the
// chain is a single scale x10 step that returns point-for-point.
func TestTransformTimeSeries_SourceFromStore(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	body := `{
	  "source": {"objectType":"sensor","primaryKey":"s1","property":"temperature"},
	  "transforms": [{"op":"scale","params":{"factor":10}}]
	}`
	req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeTransform(t, rec.Body.Bytes())
	if len(resp.Points) != 3 {
		t.Fatalf("len = %d, want 3 (seed has 3 points)", len(resp.Points))
	}
	wantVals := []float64{210.0, 225.0, 235.0} // seed × 10
	for i, want := range wantVals {
		if v, ok := resp.Points[i].Value.(float64); !ok || math.Abs(v-want) > 1e-9 {
			t.Errorf("[%d] value = %v, want %v", i, resp.Points[i].Value, want)
		}
	}
}

// TestTransformTimeSeries_ChainedSteps verifies that multiple steps
// compose in order. scale(2) then sma(3) on [10,20,30,40] yields:
// scale -> [20,40,60,80]; sma3 -> [40,60].
func TestTransformTimeSeries_ChainedSteps(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	body := `{
	  "points": [
	    {"time":"2026-04-01T00:00:00Z","value":10},
	    {"time":"2026-04-01T00:01:00Z","value":20},
	    {"time":"2026-04-01T00:02:00Z","value":30},
	    {"time":"2026-04-01T00:03:00Z","value":40}
	  ],
	  "transforms": [
	    {"op":"scale","params":{"factor":2}},
	    {"op":"sma","params":{"window":3}}
	  ]
	}`
	req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeTransform(t, rec.Body.Bytes())
	if len(resp.Points) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Points))
	}
	wantVals := []float64{40.0, 60.0}
	for i, want := range wantVals {
		if v, ok := resp.Points[i].Value.(float64); !ok || math.Abs(v-want) > 1e-9 {
			t.Errorf("[%d] value = %v, want %v", i, resp.Points[i].Value, want)
		}
	}
}

// TestTransformTimeSeries_ResampleAggregations covers the resample step
// over the inline-points path, exercising both bucketing and the
// avg|sum|min|max|count agg switch.
func TestTransformTimeSeries_ResampleAggregations(t *testing.T) {
	pts := `[
	  {"time":"2026-04-01T00:00:00Z","value":1},
	  {"time":"2026-04-01T00:00:30Z","value":2},
	  {"time":"2026-04-01T00:01:00Z","value":3},
	  {"time":"2026-04-01T00:01:30Z","value":4}
	]`
	cases := []struct {
		agg  string
		want []float64
	}{
		{"avg", []float64{1.5, 3.5}},
		{"sum", []float64{3, 7}},
		{"min", []float64{1, 3}},
		{"max", []float64{2, 4}},
		{"count", []float64{2, 2}},
	}
	for _, tc := range cases {
		t.Run(tc.agg, func(t *testing.T) {
			r, _ := setupTimeSeriesTest(t)
			body := `{"points":` + pts + `,"transforms":[{"op":"resample","params":{"interval":"1m","agg":"` + tc.agg + `"}}]}`
			req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			resp := decodeTransform(t, rec.Body.Bytes())
			if len(resp.Points) != 2 {
				t.Fatalf("len = %d want 2", len(resp.Points))
			}
			for i, w := range tc.want {
				v, _ := resp.Points[i].Value.(float64)
				if math.Abs(v-w) > 1e-9 {
					t.Errorf("[%d] got %v want %v", i, v, w)
				}
			}
		})
	}
}

// TestTransformTimeSeries_RejectsAmbiguousBody exercises the
// either-source-or-points (not-both / not-neither) and missing-transforms
// guards.
func TestTransformTimeSeries_RejectsInvalidBody(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	for _, body := range []string{
		// Both source and points
		`{"source":{"objectType":"sensor","primaryKey":"s1","property":"temperature"},
		  "points":[{"time":"2026-04-01T00:00:00Z","value":1}],
		  "transforms":[{"op":"diff"}]}`,
		// Neither
		`{"transforms":[{"op":"diff"}]}`,
		// No transforms
		`{"points":[{"time":"2026-04-01T00:00:00Z","value":1}]}`,
		// Garbage JSON
		`{`,
	} {
		req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400 (resp=%s)", body, rec.Code, rec.Body.String())
		}
	}
}

// TestTransformTimeSeries_RejectsInvalidStep ensures errors from the
// chain engine surface as 400 with TimeSeriesTransformInvalidStep.
func TestTransformTimeSeries_RejectsInvalidStep(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	body := `{
	  "points":[{"time":"2026-04-01T00:00:00Z","value":1}],
	  "transforms":[{"op":"sma","params":{"window":-1}}]
	}`
	req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

// TestTransformTimeSeries_RejectsObjectNotFound exercises the
// store-resolved branch when the source object is missing.
func TestTransformTimeSeries_RejectsObjectNotFound(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	body := `{
	  "source":{"objectType":"sensor","primaryKey":"does-not-exist","property":"temperature"},
	  "transforms":[{"op":"diff"}]
	}`
	req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404, body=%s", rec.Code, rec.Body.String())
	}
}

// TestTransformTimeSeries_AllOpsSmoke applies every supported op in one
// chain so a future op-name typo or registration regression is caught.
func TestTransformTimeSeries_AllOpsSmoke(t *testing.T) {
	r, _ := setupTimeSeriesTest(t)

	// 6 minute-spaced points, identity-ish payload.
	body := `{
	  "points": [
	    {"time":"2026-04-01T00:00:00Z","value":1},
	    {"time":"2026-04-01T00:01:00Z","value":2},
	    {"time":"2026-04-01T00:02:00Z","value":3},
	    {"time":"2026-04-01T00:03:00Z","value":4},
	    {"time":"2026-04-01T00:04:00Z","value":5},
	    {"time":"2026-04-01T00:05:00Z","value":6}
	  ],
	  "transforms": [
	    {"op":"scale","params":{"factor":1,"offset":0}},
	    {"op":"sma","params":{"window":2}},
	    {"op":"ema","params":{"alpha":0.5}},
	    {"op":"diff"},
	    {"op":"resample","params":{"interval":"5m","agg":"sum"}}
	  ]
	}`
	req := httptest.NewRequest(http.MethodPost, transformPath, bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeTransform(t, rec.Body.Bytes())
	// Each op preserves at least sometimes-empty output but the chain
	// above is sized so the resample bucket aggregates non-empty data.
	if len(resp.Points) == 0 {
		t.Errorf("expected non-empty result")
	}
}
