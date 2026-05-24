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

// TestBDD_GetQueryTypesByRidBatch closes the 8-of-8 batch-get-by-RID
// symmetry (round 89). Rounds 73/75/77/79/81/83/85/87 took the
// convention from the four core metadata kinds (objectTypes,
// actionTypes, linkTypes, interfaceTypes) through the next layer
// (valueTypes, sharedPropertyTypes, typeGroups). QueryTypes — the
// predefined filter+aggregation combos stored as metadata — was
// the eighth and last metadata kind on the OMS surface without
// a batch surface. A saved-query palette rendering N saved queries
// previously needed N round-trips.
//
// Wire shape (mirror of the established pattern):
//
//   POST /api/v2/ontologies/{ontologyApiName}/queryTypes/getByRidBatch
//   {"rids": ["qt-1", "qt-2", "qt-3"]}
//     200 + {"data": [QueryType, QueryType, QueryType]}
//         missing RIDs SKIPPED SILENTLY — same convention shared
//         with the seven other batch surfaces.
//
// Scenarios:
//   - Three RIDs all resolve.
//   - Mixed known + unknown: unknowns drop silently.
//   - Empty input: 200 + {"data": []}.
//   - Malformed body: 400 InvalidRequestBody.
//   - Response shape is {data: [...]} envelope.
func TestBDD_GetQueryTypesByRidBatch(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"

	newServer := func(t *testing.T, qts []oms.QueryType) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		repo.queryTypes = append(repo.queryTypes, qts...)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/queryTypes/getByRidBatch",
			handler.GetQueryTypesByRidBatchV2)
		return r
	}

	doPost := func(t *testing.T, r *chi.Mux, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/queryTypes/getByRidBatch",
			bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Three RIDs all resolve to three rows", func(t *testing.T) {
		qts := []oms.QueryType{
			{RID: "qt-1", OntologyRID: ontRID, APIName: "topCustomers", DisplayName: "Top Customers", Status: "ACTIVE"},
			{RID: "qt-2", OntologyRID: ontRID, APIName: "openOrders", DisplayName: "Open Orders", Status: "ACTIVE"},
			{RID: "qt-3", OntologyRID: ontRID, APIName: "lateShipments", DisplayName: "Late Shipments", Status: "ACTIVE"},
		}
		r := newServer(t, qts)
		rec := doPost(t, r, map[string]any{"rids": []string{"qt-1", "qt-2", "qt-3"}})
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
		qts := []oms.QueryType{
			{RID: "qt-1", OntologyRID: ontRID, APIName: "topCustomers", Status: "ACTIVE"},
			{RID: "qt-2", OntologyRID: ontRID, APIName: "openOrders", Status: "ACTIVE"},
		}
		r := newServer(t, qts)
		rec := doPost(t, r, map[string]any{
			"rids": []string{"qt-1", "ghost-qt-99", "qt-2", "another-ghost"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		var resp struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 2 {
			t.Fatalf("len(data)=%d, want 2 (only qt-1 + qt-2 resolve)", len(resp.Data))
		}
		gotRids := map[string]bool{}
		for _, e := range resp.Data {
			gotRids[e["rid"].(string)] = true
		}
		if !gotRids["qt-1"] || !gotRids["qt-2"] {
			t.Errorf("missing expected RIDs in response: %v", resp.Data)
		}
		if gotRids["ghost-qt-99"] || gotRids["another-ghost"] {
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
			t.Errorf("data is nil, want empty array")
		}
		if len(resp.Data) != 0 {
			t.Errorf("len(data)=%d, want 0", len(resp.Data))
		}
	})

	t.Run("Malformed body returns 400 InvalidRequestBody", func(t *testing.T) {
		r := newServer(t, nil)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/queryTypes/getByRidBatch",
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
		qts := []oms.QueryType{{RID: "qt-1", OntologyRID: ontRID, APIName: "topCustomers", Status: "ACTIVE"}}
		r := newServer(t, qts)
		rec := doPost(t, r, map[string]any{"rids": []string{"qt-1"}})
		body := rec.Body.String()
		if len(body) == 0 || body[0] != '{' {
			t.Errorf("body starts with %q, want '{' (object envelope); body=%s",
				string(body[0]), body)
		}
	})
}
