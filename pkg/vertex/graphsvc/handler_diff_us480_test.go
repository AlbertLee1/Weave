// US-480 — HTTP wire-level coverage for GET /api/vertex/v1/graphs/{rid}/diff.
// The unit cases here exercise the handler/router/JSONPatch composition
// over MemRepo (no PG). BDD coverage with real PG sits in
// test/integration/vertex_graph_diff_us480_bdd_test.go.

package graphsvc_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

func TestDiffHandler_Given_TwoVersions_When_GET_Then_ReturnsJSONPatchOps_US480(t *testing.T) {
	r, repo, _ := newTestHandler(t)
	g, err := repo.Create(nil, "ri.ontology.main.ontology.vtx", "diff fixture", "tester",
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"}],"edges":[]}`), true)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Update(nil, g.RID,
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"},{"id":"L2","objectType":"Order"}],"edges":[]}`)); err != nil {
		t.Fatalf("update: %v", err)
	}

	w := doRequest(t, r, http.MethodGet,
		"/api/vertex/v1/graphs/"+g.RID+"/diff?from=1&to=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		RID  string             `json:"rid"`
		From int                `json:"from"`
		To   int                `json:"to"`
		Ops  []graphsvc.PatchOp `json:"ops"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body=%s", err, w.Body.String())
	}
	if resp.RID != g.RID || resp.From != 1 || resp.To != 2 {
		t.Errorf("envelope mismatch: %+v", resp)
	}
	if len(resp.Ops) != 1 || resp.Ops[0].Op != "add" || resp.Ops[0].Path != "/layers/L2" {
		t.Errorf("ops=%+v, want one add at /layers/L2", resp.Ops)
	}
}

func TestDiffHandler_Given_MissingFromParam_When_GET_Then_400MissingVersion_US480(t *testing.T) {
	r, repo, _ := newTestHandler(t)
	g, _ := repo.Create(nil, "ri.ontology.main.ontology.vtx", "fixture", "tester",
		json.RawMessage(`{"layers":[]}`), true)

	w := doRequest(t, r, http.MethodGet,
		"/api/vertex/v1/graphs/"+g.RID+"/diff?to=1", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if name, _ := resp["errorName"].(string); name != "MissingVersion" {
		t.Errorf("errorName=%q, want MissingVersion; body=%s", name, w.Body.String())
	}
}

func TestDiffHandler_Given_InvalidVersion_When_GET_Then_400InvalidVersion_US480(t *testing.T) {
	r, repo, _ := newTestHandler(t)
	g, _ := repo.Create(nil, "ri.ontology.main.ontology.vtx", "fixture", "tester",
		json.RawMessage(`{"layers":[]}`), true)

	w := doRequest(t, r, http.MethodGet,
		"/api/vertex/v1/graphs/"+g.RID+"/diff?from=zero&to=1", nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, want 400; body=%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if name, _ := resp["errorName"].(string); name != "InvalidVersion" {
		t.Errorf("errorName=%q, want InvalidVersion; body=%s", name, w.Body.String())
	}
}

func TestDiffHandler_Given_UnknownVersion_When_GET_Then_404VersionNotFound_US480(t *testing.T) {
	r, repo, _ := newTestHandler(t)
	g, _ := repo.Create(nil, "ri.ontology.main.ontology.vtx", "fixture", "tester",
		json.RawMessage(`{"layers":[]}`), true)

	w := doRequest(t, r, http.MethodGet,
		"/api/vertex/v1/graphs/"+g.RID+"/diff?from=1&to=99", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404; body=%s", w.Code, w.Body.String())
	}
}

func TestDiffHandler_Given_RemoveEdgeAndReplaceLayerProperty_When_GET_Then_TwoOpsSortedByPath_US480(t *testing.T) {
	r, repo, _ := newTestHandler(t)
	g, _ := repo.Create(nil, "ri.ontology.main.ontology.vtx", "fixture", "tester",
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Customer"}],"edges":[{"id":"E1","source":"L1","target":"L1"}]}`), true)
	if _, err := repo.Update(nil, g.RID,
		json.RawMessage(`{"layers":[{"id":"L1","objectType":"Vendor"}],"edges":[]}`)); err != nil {
		t.Fatalf("update: %v", err)
	}

	w := doRequest(t, r, http.MethodGet,
		"/api/vertex/v1/graphs/"+g.RID+"/diff?from=1&to=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Ops []graphsvc.PatchOp `json:"ops"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Ops) != 2 {
		t.Fatalf("len(ops)=%d, want 2; ops=%+v", len(resp.Ops), resp.Ops)
	}
	// Sorted by path: /edges/E1 < /layers/L1/objectType.
	if resp.Ops[0].Path != "/edges/E1" || resp.Ops[0].Op != "remove" {
		t.Errorf("ops[0]=%+v, want {remove /edges/E1}", resp.Ops[0])
	}
	if resp.Ops[1].Path != "/layers/L1/objectType" || resp.Ops[1].Op != "replace" || resp.Ops[1].Value != "Vendor" {
		t.Errorf("ops[1]=%+v, want {replace /layers/L1/objectType Vendor}", resp.Ops[1])
	}
}
