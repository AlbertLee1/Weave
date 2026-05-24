package oss_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oss"
)

// TestBDD_CountObjects_WhereFilter_FoundryAlignment covers the 1:1
// Foundry OSv2 alignment gap on POST .../objects/{objectType}/count.
//
// Foundry's count endpoint accepts an optional request body
// `{"where": ...}` and returns the count of MATCHING objects, not the
// total document count of the type. Weave's earlier implementation
// always returned the full count via indexMgr.DocCount and ignored any
// body — which both broke wire alignment AND silently bypassed any
// row-level policy filter that would have been applied on the search
// path. Operators counting filtered subsets (e.g. "how many active
// employees?") had no choice but to fall back to a full /search with
// pageSize=1 and read totalCount.
//
// The new behaviour:
//
//   - Empty body keeps the existing fast path (DocCount). Backwards-
//     compatible for SDKs that never sent a body.
//   - {"where": {...}} runs the same where-clause → Bleve query pipeline
//     SearchObjects uses, but with Size=0 so Bleve returns the total
//     match count without paying to materialise documents.
//   - Malformed where surfaces as 400 CountObjectsFailed, mirroring
//     SearchObjects' error contract.
func TestBDD_CountObjects_WhereFilter_FoundryAlignment(t *testing.T) {
	doCount := func(t *testing.T, body string) *httptest.ResponseRecorder {
		t.Helper()
		svc, _, _, _ := setupOSSTest(t)
		h := oss.NewHandler(svc)
		r := chi.NewRouter()
		h.RegisterRoutes(r)

		var bodyReader *bytes.Reader
		if body != "" {
			bodyReader = bytes.NewReader([]byte(body))
		}
		var req *http.Request
		if bodyReader != nil {
			req = httptest.NewRequest("POST",
				"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/count", bodyReader)
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest("POST",
				"/api/v2/ontologies/"+testOntologyRID+"/objects/employee/count", nil)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Empty body keeps the backward-compatible full count", func(t *testing.T) {
		// setupOSSTest seeds 3 employees: alice(30), bob(25), charlie(35).
		rec := doCount(t, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Count != 3 {
			t.Errorf("empty-body count = %d, want 3 (full count fast path)", resp.Count)
		}
	})

	t.Run("Empty JSON object body still counts everything (no filter == full)", func(t *testing.T) {
		// {} with no where field must be equivalent to no body — the SDK
		// could legitimately serialize an unused options struct that
		// produces {} on the wire; treating it as a different code path
		// would surprise the caller.
		rec := doCount(t, `{}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Count != 3 {
			t.Errorf("empty-object count = %d, want 3", resp.Count)
		}
	})

	t.Run("Where filter limits the count to matching objects", func(t *testing.T) {
		// age > 28 → matches alice(30) and charlie(35); bob(25) excluded.
		rec := doCount(t, `{"where":{"type":"gt","field":"age","value":28}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Count != 2 {
			t.Errorf("filtered count = %d, want 2 (alice + charlie pass age>28)", resp.Count)
		}
	})

	t.Run("Where filter that matches nothing returns count=0", func(t *testing.T) {
		rec := doCount(t, `{"where":{"type":"eq","field":"name","value":"nobody"}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Count != 0 {
			t.Errorf("no-match count = %d, want 0", resp.Count)
		}
	})

	t.Run("Where filter on a single equality still counts both pages of hits", func(t *testing.T) {
		// deptId=d1 → matches alice + bob; verifies the count returns
		// the full match total, not a page-limited result, even though
		// the underlying bleve.NewSearchRequest is run with Size=0.
		rec := doCount(t, `{"where":{"type":"eq","field":"deptId","value":"d1"}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Count != 2 {
			t.Errorf("deptId=d1 count = %d, want 2 (alice + bob)", resp.Count)
		}
	})
}
