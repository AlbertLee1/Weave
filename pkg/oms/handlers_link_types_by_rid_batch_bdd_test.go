package oms_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/liyang/weave/pkg/oms"
)

// TestBDD_GetLinkTypesByRidBatch covers the round-79 symmetry fix
// for the batch-get-by-RID surface. ObjectTypes and ActionTypes
// each have getByRidBatch endpoints; linkTypes did not. SDK
// callers rendering N link metadatas (Foundry ObjectList link
// columns, link-property tooltips, scenario diff badges) had to
// issue N parallel GET /linkTypes/byRid/{rid} round-trips just
// to label a list of edge RIDs.
//
// Wire shape (mirror of the existing actionTypes pattern):
//
//	POST /api/v2/ontologies/{ontologyApiName}/linkTypes/getByRidBatch
//	{"rids": ["lt-1", "lt-2", "lt-3"]}
//	  200 + {"data": [LinkType, LinkType, LinkType]}
//	      missing RIDs are SKIPPED SILENTLY — the response carries
//	      only the resolvable ones, matching the existing
//	      getByRidBatch convention so SDK partial-render logic
//	      stays portable across object/action/link batch surfaces.
//
// Scenarios:
//   - Three RIDs all resolve: response carries all three rows in
//     request order.
//   - Mixed known + unknown RIDs: unknowns drop silently, knowns
//     come through (matches actionTypes/objectTypes behavior —
//     callers infer "missing == not in array").
//   - Empty input array: 200 + {"data": []} (non-nil empty —
//     no wasted round-trip on the "no rows visible" page state).
//   - Malformed body returns 400 InvalidRequestBody.
//   - Response shape is {data: [...]} envelope (regression guard
//     for future pagination — bare array would lock us out).
func TestBDD_GetLinkTypesByRidBatch(t *testing.T) {
	const (
		ontRID  = "ri.ontology.main.ontology.1"
		otCust  = "ri.objecttype.main.Customer"
		otOrder = "ri.objecttype.main.Order"
	)

	newServer := func(t *testing.T, links []oms.LinkType) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		repo.linkTypes = append(repo.linkTypes, links...)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/linkTypes/getByRidBatch",
			handler.GetLinkTypesByRidBatchV2)
		return r
	}

	doPost := func(t *testing.T, r *chi.Mux, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/linkTypes/getByRidBatch",
			bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Three RIDs all resolve to three rows", func(t *testing.T) {
		links := []oms.LinkType{
			{RID: "lt-1", APIName: "owns", OntologyRID: ontRID,
				SourceObjectType: otCust, TargetObjectType: otOrder, Cardinality: "ONE_TO_MANY"},
			{RID: "lt-2", APIName: "billedTo", OntologyRID: ontRID,
				SourceObjectType: otOrder, TargetObjectType: otCust, Cardinality: "MANY_TO_ONE"},
			{RID: "lt-3", APIName: "manages", OntologyRID: ontRID,
				SourceObjectType: otCust, TargetObjectType: otCust, Cardinality: "ONE_TO_MANY"},
		}
		r := newServer(t, links)
		rec := doPost(t, r, map[string]any{"rids": []string{"lt-1", "lt-2", "lt-3"}})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 3 {
			t.Fatalf("len(data)=%d, want 3; body=%s", len(resp.Data), rec.Body.String())
		}
	})

	t.Run("Mixed known + unknown RIDs: unknowns drop silently", func(t *testing.T) {
		links := []oms.LinkType{
			{RID: "lt-1", APIName: "owns", OntologyRID: ontRID,
				SourceObjectType: otCust, TargetObjectType: otOrder, Cardinality: "ONE_TO_MANY"},
			{RID: "lt-2", APIName: "billedTo", OntologyRID: ontRID,
				SourceObjectType: otOrder, TargetObjectType: otCust, Cardinality: "MANY_TO_ONE"},
		}
		r := newServer(t, links)
		rec := doPost(t, r, map[string]any{
			"rids": []string{"lt-1", "ghost-lt-99", "lt-2", "another-ghost"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		var resp struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 2 {
			t.Fatalf("len(data)=%d, want 2 (only lt-1 + lt-2 resolve); body=%s",
				len(resp.Data), rec.Body.String())
		}
		gotRids := map[string]bool{}
		for _, l := range resp.Data {
			gotRids[l["rid"].(string)] = true
		}
		if !gotRids["lt-1"] || !gotRids["lt-2"] {
			t.Errorf("missing expected RIDs in response: %v", resp.Data)
		}
		if gotRids["ghost-lt-99"] || gotRids["another-ghost"] {
			t.Errorf("ghost RID leaked into response: %v", resp.Data)
		}
	})

	t.Run("Empty input returns 200 + non-nil empty data", func(t *testing.T) {
		r := newServer(t, nil)
		rec := doPost(t, r, map[string]any{"rids": []string{}})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		var resp struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp.Data == nil {
			t.Errorf("data is nil, want empty array (SPA iterates without nil-check)")
		}
		if len(resp.Data) != 0 {
			t.Errorf("len(data)=%d, want 0", len(resp.Data))
		}
	})

	t.Run("Malformed body returns 400 InvalidRequestBody", func(t *testing.T) {
		r := newServer(t, nil)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/linkTypes/getByRidBatch",
			bytes.NewReader([]byte("{not-json")))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d, want 400; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if body["errorName"] != "InvalidRequestBody" {
			t.Errorf("errorName=%v, want InvalidRequestBody", body["errorName"])
		}
	})

	t.Run("Response shape is {data: [...]} envelope", func(t *testing.T) {
		links := []oms.LinkType{
			{RID: "lt-1", APIName: "owns", OntologyRID: ontRID,
				SourceObjectType: otCust, TargetObjectType: otOrder, Cardinality: "ONE_TO_MANY"},
		}
		r := newServer(t, links)
		rec := doPost(t, r, map[string]any{"rids": []string{"lt-1"}})
		body := rec.Body.String()
		if len(body) == 0 || body[0] != '{' {
			t.Errorf("body starts with %q, want '{' (object envelope); body=%s",
				string(body[0]), body)
		}
	})
}
