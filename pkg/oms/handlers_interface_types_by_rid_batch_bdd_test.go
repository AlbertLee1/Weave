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

// TestBDD_GetInterfaceTypesByRidBatch covers the round-81 symmetry
// continuation. ObjectTypes (round-old), ActionTypes (round-old),
// and LinkTypes (round 79) all have getByRidBatch endpoints. Round
// 81 closes the gap for InterfaceTypes — the fourth and final
// metadata kind in the OMS surface that previously needed N
// round-trips to label N interface RIDs.
//
// Wire shape (mirror of the existing actionTypes/linkTypes pattern):
//
//	POST /api/v2/ontologies/{ontologyApiName}/interfaceTypes/getByRidBatch
//	{"rids": ["if-1", "if-2", "if-3"]}
//	  200 + {"data": [Interface, Interface, Interface]}
//	      missing RIDs are SKIPPED SILENTLY — matches the
//	      established round-79 convention so SDK partial-render
//	      logic stays portable across all four batch surfaces.
//
// Scenarios:
//   - Three RIDs all resolve: response carries all three.
//   - Mixed known + unknown: unknowns drop silently.
//   - Empty input: 200 + {"data": []}.
//   - Malformed body: 400 InvalidRequestBody.
//   - Response shape is {data: [...]} envelope.
func TestBDD_GetInterfaceTypesByRidBatch(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"

	newServer := func(t *testing.T, interfaces []oms.Interface) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		repo.interfaces = append(repo.interfaces, interfaces...)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/interfaceTypes/getByRidBatch",
			handler.GetInterfaceTypesByRidBatchV2)
		return r
	}

	doPost := func(t *testing.T, r *chi.Mux, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/interfaceTypes/getByRidBatch",
			bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Three RIDs all resolve to three rows", func(t *testing.T) {
		ifs := []oms.Interface{
			{RID: "if-1", OntologyRID: ontRID, APIName: "HasOwner", DisplayName: "Has Owner"},
			{RID: "if-2", OntologyRID: ontRID, APIName: "Searchable", DisplayName: "Searchable"},
			{RID: "if-3", OntologyRID: ontRID, APIName: "Auditable", DisplayName: "Auditable"},
		}
		r := newServer(t, ifs)
		rec := doPost(t, r, map[string]any{"rids": []string{"if-1", "if-2", "if-3"}})
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
		ifs := []oms.Interface{
			{RID: "if-1", OntologyRID: ontRID, APIName: "HasOwner"},
			{RID: "if-2", OntologyRID: ontRID, APIName: "Searchable"},
		}
		r := newServer(t, ifs)
		rec := doPost(t, r, map[string]any{
			"rids": []string{"if-1", "ghost-if-99", "if-2", "another-ghost"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		var resp struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 2 {
			t.Fatalf("len(data)=%d, want 2 (only if-1 + if-2 resolve)", len(resp.Data))
		}
		gotRids := map[string]bool{}
		for _, e := range resp.Data {
			gotRids[e["rid"].(string)] = true
		}
		if !gotRids["if-1"] || !gotRids["if-2"] {
			t.Errorf("missing expected RIDs in response: %v", resp.Data)
		}
		if gotRids["ghost-if-99"] || gotRids["another-ghost"] {
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
			"/api/v2/ontologies/"+ontRID+"/interfaceTypes/getByRidBatch",
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
		ifs := []oms.Interface{{RID: "if-1", OntologyRID: ontRID, APIName: "HasOwner"}}
		r := newServer(t, ifs)
		rec := doPost(t, r, map[string]any{"rids": []string{"if-1"}})
		body := rec.Body.String()
		if len(body) == 0 || body[0] != '{' {
			t.Errorf("body starts with %q, want '{' (object envelope); body=%s",
				string(body[0]), body)
		}
	})
}
