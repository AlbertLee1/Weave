// VTX-016 — SystemGraph lifecycle E2E smoke.
//
// Drives the Phase-2 Vertex graph surface (VTX-009 ~ VTX-015) end-to-end
// through the full chi HTTP stack — no PostgreSQL, the in-memory Repo +
// TemplateStore + ShareLinkStore stand in for the PG implementations. The
// goal is to prove the lifecycle "create → save → bump version → duplicate
// → fetch history" stays green together AND completes inside the 1-second
// wall-clock budget the BDD calls for.
//
// One large test function rather than many small ones: the BDD here is the
// full chain, and asserting against intermediate state in each step keeps
// the failure surface localised (the test stops at the first bad step) while
// keeping the timing measurement on the whole flow.
package vertex_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/vertex/graphsvc"
)

const (
	ontologyRIDForLifecycle = "ri.ontology.main.ontology.vtx-smoke"
	graphLifecycleBudget    = time.Second
)

// httpDo encodes body (when non-nil), runs the request through the router,
// and returns the recorder. Centralised so each step in the lifecycle is one
// line.
func httpDo(t *testing.T, r chi.Router, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode %s %s body: %v", method, path, err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// decodeMap is a tiny helper that fails the test on bad JSON instead of
// silently leaving the assertion vague.
func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode JSON: %v; body: %s", err, w.Body.String())
	}
	return m
}

// validGraphPayload returns a payload that satisfies the VTX-011 JSON Schema
// even though the lifecycle test does not install the validator. Keeping the
// payload schema-clean future-proofs the smoke against a later wiring change
// that turns the validator on by default.
func validGraphPayload(layerID string) map[string]any {
	return map[string]any{
		"layers": []any{
			map[string]any{"id": layerID, "objectTypeRid": "ri.oms.main.object-type.airport"},
		},
		"edges":     []any{},
		"positions": map[string]any{},
	}
}

func TestSystemGraphLifecycle_Given_FreshStack_When_CreateSaveBumpDuplicateHistory_Then_AllStepsOKUnderOneSecond(t *testing.T) {
	// Wire the full HTTP surface: mem repo + mem template store + chi router.
	// This mirrors what cmd/server/main.go does in degraded-mode boots where
	// the PG pool is unavailable, so the lifecycle exercises the same handler
	// code paths a real boot does.
	repo := graphsvc.NewMemRepo()
	templates := graphsvc.NewMemTemplateStore()
	h := graphsvc.NewHandler(repo, templates)
	h.SetShareLinkStore(graphsvc.NewMemShareLinkStore())
	router := chi.NewRouter()
	h.RegisterRoutes(router)

	start := time.Now()

	// --- Step 1: create graph (v=1) ---
	createBody := map[string]any{
		"ontologyRid": ontologyRIDForLifecycle,
		"name":        "Lifecycle Map",
		"versioned":   true,
		"payload":     validGraphPayload("L1"),
	}
	createResp := httpDo(t, router, http.MethodPost, "/api/vertex/v1/graphs", createBody)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, want 201; body: %s", createResp.Code, createResp.Body.String())
	}
	created := decodeMap(t, createResp)
	graphRID, _ := created["rid"].(string)
	if !strings.HasPrefix(graphRID, "ri.vertex.main.graph.") {
		t.Fatalf("create: rid = %q, want ri.vertex.main.graph.* prefix", graphRID)
	}
	if v, _ := created["version"].(float64); v != 1 {
		t.Fatalf("create: version = %v, want 1", created["version"])
	}

	// --- Step 2: save (PUT → v=2). "保存" in BDD == full PUT save. ---
	saveResp := httpDo(t, router, http.MethodPut, "/api/vertex/v1/graphs/"+graphRID, map[string]any{
		"payload": validGraphPayload("L2"),
	})
	if saveResp.Code != http.StatusOK {
		t.Fatalf("save: status = %d, want 200; body: %s", saveResp.Code, saveResp.Body.String())
	}
	if v, _ := decodeMap(t, saveResp)["version"].(float64); v != 2 {
		t.Fatalf("save: version = %v after first PUT, want 2", v)
	}

	// --- Step 3: bump version again (PUT → v=3). "加版本" in BDD. ---
	bumpResp := httpDo(t, router, http.MethodPut, "/api/vertex/v1/graphs/"+graphRID, map[string]any{
		"payload": validGraphPayload("L3"),
	})
	if bumpResp.Code != http.StatusOK {
		t.Fatalf("bump: status = %d, want 200; body: %s", bumpResp.Code, bumpResp.Body.String())
	}
	if v, _ := decodeMap(t, bumpResp)["version"].(float64); v != 3 {
		t.Fatalf("bump: version = %v after second PUT, want 3", v)
	}

	// --- Step 4: duplicate ---
	dupResp := httpDo(t, router, http.MethodPost, "/api/vertex/v1/graphs/"+graphRID+"/duplicate", nil)
	if dupResp.Code != http.StatusCreated {
		t.Fatalf("duplicate: status = %d, want 201; body: %s", dupResp.Code, dupResp.Body.String())
	}
	dup := decodeMap(t, dupResp)
	dupRID, _ := dup["rid"].(string)
	if dupRID == graphRID || !strings.HasPrefix(dupRID, "ri.vertex.main.graph.") {
		t.Fatalf("duplicate: dupRid = %q (source = %q); want fresh ri.vertex.main.graph.* rid", dupRID, graphRID)
	}
	if dv, _ := dup["version"].(float64); dv != 1 {
		t.Fatalf("duplicate: version = %v, want 1 (history reset)", dv)
	}

	// --- Step 5: fetch history. Source has 3 versions; duplicate has 1. ---
	historyResp := httpDo(t, router, http.MethodGet, "/api/vertex/v1/graphs/"+graphRID+"/history", nil)
	if historyResp.Code != http.StatusOK {
		t.Fatalf("history: status = %d, want 200; body: %s", historyResp.Code, historyResp.Body.String())
	}
	history := decodeMap(t, historyResp)
	versions, _ := history["versions"].([]any)
	if len(versions) != 3 {
		t.Fatalf("history: source graph has %d versions, want 3", len(versions))
	}

	dupHistoryResp := httpDo(t, router, http.MethodGet, "/api/vertex/v1/graphs/"+dupRID+"/history", nil)
	if dupHistoryResp.Code != http.StatusOK {
		t.Fatalf("duplicate history: status = %d, want 200; body: %s", dupHistoryResp.Code, dupHistoryResp.Body.String())
	}
	dupVersions, _ := decodeMap(t, dupHistoryResp)["versions"].([]any)
	if len(dupVersions) != 1 {
		t.Fatalf("duplicate history: dup has %d versions, want 1 (independent history)", len(dupVersions))
	}

	// --- BDD: the full chain finishes inside 1 second. ---
	elapsed := time.Since(start)
	if elapsed > graphLifecycleBudget {
		t.Fatalf("lifecycle took %s, want ≤ %s", elapsed, graphLifecycleBudget)
	}
	t.Logf("lifecycle (create + 2 PUTs + duplicate + 2 history GETs) finished in %s", elapsed)
}
