package oss_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss"
	"github.com/liyang/weave/pkg/oss/aggregation"
)

// Foundry parity BDD: accuracy=REQUIRE_ACCURATE means "error when exactness
// cannot be guaranteed", not "silently return an APPROXIMATE badge".
//
// Given an index whose scan is truncated by MaxDocScanSize (3 employee rows,
// engine scan cap of 1), a scan-based metric (avg on age) can only produce an
// APPROXIMATE result.
//
//   - When the caller demands accuracy=REQUIRE_ACCURATE
//     Then the endpoint responds 400 with errorName AccuracyNotGuaranteed
//     (errorCode INVALID_ARGUMENT) instead of a 200 APPROXIMATE badge.
//   - When the caller uses the default (ALLOW_APPROXIMATE)
//     Then the endpoint responds 200 with accuracy=APPROXIMATE, unchanged.
func TestBDD_FoundryParity_RequireAccurateErrorsWhenScanTruncated(t *testing.T) {
	svc, mgr, _, _ := setupOSSTest(t)

	h := oss.NewHandler(svc)
	// Scan cap of 1 over 3 indexed employees forces the avg scan to truncate.
	h.SetAggregation(&aggregation.Engine{MaxDocScanSize: 1}, mgr)

	r := chi.NewRouter()
	h.RegisterRoutes(r)

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/aggregate",
			strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("REQUIRE_ACCURATE -> 400 AccuracyNotGuaranteed", func(t *testing.T) {
		rec := post(`{
			"accuracy":"REQUIRE_ACCURATE",
			"aggregation":[{"type":"avg","field":"age","name":"avgAge"}]
		}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
		}
		var apiErr struct {
			ErrorCode string `json:"errorCode"`
			ErrorName string `json:"errorName"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &apiErr); err != nil {
			t.Fatalf("unmarshal error body: %v", err)
		}
		if apiErr.ErrorCode != "INVALID_ARGUMENT" {
			t.Errorf("errorCode = %q, want INVALID_ARGUMENT", apiErr.ErrorCode)
		}
		if apiErr.ErrorName != "AccuracyNotGuaranteed" {
			t.Errorf("errorName = %q, want AccuracyNotGuaranteed", apiErr.ErrorName)
		}
	})

	t.Run("ALLOW_APPROXIMATE default -> 200 APPROXIMATE", func(t *testing.T) {
		rec := post(`{
			"aggregation":[{"type":"avg","field":"age","name":"avgAge"}]
		}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		var resp aggregation.AggregationResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Accuracy != "APPROXIMATE" {
			t.Errorf("accuracy = %q, want APPROXIMATE", resp.Accuracy)
		}
		if _, ok := metricByName(resp.Data[0], "avgAge"); !ok {
			t.Errorf("avgAge metric missing from approximate response: %+v", resp.Data[0].Metrics)
		}
	})

	t.Run("explicit ALLOW_APPROXIMATE -> 200 APPROXIMATE", func(t *testing.T) {
		rec := post(`{
			"accuracy":"ALLOW_APPROXIMATE",
			"aggregation":[{"type":"avg","field":"age","name":"avgAge"}]
		}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
		}
		var resp aggregation.AggregationResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Accuracy != "APPROXIMATE" {
			t.Errorf("accuracy = %q, want APPROXIMATE", resp.Accuracy)
		}
	})
}
