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

// TestBDD_GetTypeGroupsByRidBatch covers the round-87 symmetry
// extension. Rounds 73/75/77/79/81/83/85 took batch-get-by-RID
// from the four core metadata kinds (objectTypes / actionTypes /
// linkTypes / interfaceTypes / valueTypes) to the next layer
// (sharedPropertyTypes). Round 87 closes the navigation-pane
// gap: TypeGroups organise ObjectTypes into categories and are
// rendered N at a time in the SPA's Browser sidebar / Explorer
// faceting controls. Without a batch surface a list view with
// 50 visible groups needed 50 round-trips to label them.
//
// Wire shape (mirror of the established pattern):
//
//   POST /api/v2/ontologies/{ontologyApiName}/typeGroups/getByRidBatch
//   {"rids": ["tg-1", "tg-2", "tg-3"]}
//     200 + {"data": [TypeGroup, TypeGroup, TypeGroup]}
//         missing RIDs SKIPPED SILENTLY — same convention shared
//         with the six other batch surfaces.
//
// Scenarios:
//   - Three RIDs all resolve.
//   - Mixed known + unknown: unknowns drop silently.
//   - Empty input: 200 + {"data": []}.
//   - Malformed body: 400 InvalidRequestBody.
//   - Response shape is {data: [...]} envelope.
func TestBDD_GetTypeGroupsByRidBatch(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"

	newServer := func(t *testing.T, tgs []oms.TypeGroup) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		repo.typeGroups = append(repo.typeGroups, tgs...)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/typeGroups/getByRidBatch",
			handler.GetTypeGroupsByRidBatchV2)
		return r
	}

	doPost := func(t *testing.T, r *chi.Mux, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/typeGroups/getByRidBatch",
			bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Three RIDs all resolve to three rows", func(t *testing.T) {
		tgs := []oms.TypeGroup{
			{RID: "tg-1", OntologyRID: ontRID, APIName: "people", DisplayName: "People", Color: "#3b82f6"},
			{RID: "tg-2", OntologyRID: ontRID, APIName: "places", DisplayName: "Places", Color: "#10b981"},
			{RID: "tg-3", OntologyRID: ontRID, APIName: "things", DisplayName: "Things", Color: "#f59e0b"},
		}
		r := newServer(t, tgs)
		rec := doPost(t, r, map[string]any{"rids": []string{"tg-1", "tg-2", "tg-3"}})
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
		tgs := []oms.TypeGroup{
			{RID: "tg-1", OntologyRID: ontRID, APIName: "people", DisplayName: "People"},
			{RID: "tg-2", OntologyRID: ontRID, APIName: "places", DisplayName: "Places"},
		}
		r := newServer(t, tgs)
		rec := doPost(t, r, map[string]any{
			"rids": []string{"tg-1", "ghost-tg-99", "tg-2", "another-ghost"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		var resp struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 2 {
			t.Fatalf("len(data)=%d, want 2 (only tg-1 + tg-2 resolve)", len(resp.Data))
		}
		gotRids := map[string]bool{}
		for _, e := range resp.Data {
			gotRids[e["rid"].(string)] = true
		}
		if !gotRids["tg-1"] || !gotRids["tg-2"] {
			t.Errorf("missing expected RIDs in response: %v", resp.Data)
		}
		if gotRids["ghost-tg-99"] || gotRids["another-ghost"] {
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
			"/api/v2/ontologies/"+ontRID+"/typeGroups/getByRidBatch",
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
		tgs := []oms.TypeGroup{{RID: "tg-1", OntologyRID: ontRID, APIName: "people"}}
		r := newServer(t, tgs)
		rec := doPost(t, r, map[string]any{"rids": []string{"tg-1"}})
		body := rec.Body.String()
		if len(body) == 0 || body[0] != '{' {
			t.Errorf("body starts with %q, want '{' (object envelope); body=%s",
				string(body[0]), body)
		}
	})
}
