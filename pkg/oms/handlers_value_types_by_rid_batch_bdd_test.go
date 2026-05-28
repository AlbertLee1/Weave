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

// TestBDD_GetValueTypesByRidBatch covers the round-83 symmetry
// completion. ObjectTypes, ActionTypes, LinkTypes (round 79) and
// InterfaceTypes (round 81) all have getByRidBatch endpoints.
// ValueTypes was the last metadata kind in the OMS surface that
// still required N round-trips to label N value-type RIDs in
// Property-editor dropdowns / unit-suggestion panels.
//
// Wire shape (mirror of the established pattern):
//
//	POST /api/v2/ontologies/{ontologyApiName}/valueTypes/getByRidBatch
//	{"rids": ["vt-1", "vt-2", "vt-3"]}
//	  200 + {"data": [ValueType, ValueType, ValueType]}
//	      missing RIDs SKIPPED SILENTLY — matches the convention
//	      shared with the other four batch surfaces so SDK
//	      partial-render logic stays portable.
//
// Scenarios:
//   - Three RIDs all resolve.
//   - Mixed known + unknown: unknowns drop silently.
//   - Empty input: 200 + {"data": []}.
//   - Malformed body: 400 InvalidRequestBody.
//   - Response shape is {data: [...]} envelope.
func TestBDD_GetValueTypesByRidBatch(t *testing.T) {
	const ontRID = "ri.ontology.main.ontology.1"

	newServer := func(t *testing.T, vts []oms.ValueType) *chi.Mux {
		t.Helper()
		repo := &mockRepo{}
		repo.valueTypes = append(repo.valueTypes, vts...)
		handler := oms.NewOMSHandler(repo)
		r := chi.NewRouter()
		r.Post("/api/v2/ontologies/{ontologyApiName}/valueTypes/getByRidBatch",
			handler.GetValueTypesByRidBatchV2)
		return r
	}

	doPost := func(t *testing.T, r *chi.Mux, body any) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost,
			"/api/v2/ontologies/"+ontRID+"/valueTypes/getByRidBatch",
			bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("Three RIDs all resolve to three rows", func(t *testing.T) {
		vts := []oms.ValueType{
			{RID: "vt-1", APIName: "EmailAddress", DisplayName: "Email Address", BaseType: "string"},
			{RID: "vt-2", APIName: "Currency", DisplayName: "Currency", BaseType: "double"},
			{RID: "vt-3", APIName: "Quantity", DisplayName: "Quantity", BaseType: "integer"},
		}
		r := newServer(t, vts)
		rec := doPost(t, r, map[string]any{"rids": []string{"vt-1", "vt-2", "vt-3"}})
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
		vts := []oms.ValueType{
			{RID: "vt-1", APIName: "EmailAddress", BaseType: "string"},
			{RID: "vt-2", APIName: "Currency", BaseType: "double"},
		}
		r := newServer(t, vts)
		rec := doPost(t, r, map[string]any{
			"rids": []string{"vt-1", "ghost-vt-99", "vt-2", "another-ghost"},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d, want 200", rec.Code)
		}
		var resp struct {
			Data []map[string]any `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if len(resp.Data) != 2 {
			t.Fatalf("len(data)=%d, want 2 (only vt-1 + vt-2 resolve)", len(resp.Data))
		}
		gotRids := map[string]bool{}
		for _, e := range resp.Data {
			gotRids[e["rid"].(string)] = true
		}
		if !gotRids["vt-1"] || !gotRids["vt-2"] {
			t.Errorf("missing expected RIDs in response: %v", resp.Data)
		}
		if gotRids["ghost-vt-99"] || gotRids["another-ghost"] {
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
			"/api/v2/ontologies/"+ontRID+"/valueTypes/getByRidBatch",
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
		vts := []oms.ValueType{{RID: "vt-1", APIName: "EmailAddress", BaseType: "string"}}
		r := newServer(t, vts)
		rec := doPost(t, r, map[string]any{"rids": []string{"vt-1"}})
		body := rec.Body.String()
		if len(body) == 0 || body[0] != '{' {
			t.Errorf("body starts with %q, want '{' (object envelope); body=%s",
				string(body[0]), body)
		}
	})
}
