package modelmesh_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/vertex/modelmesh"
)

type stubResolver struct {
	byAPIName map[string]string
}

func (s *stubResolver) ResolveOntologyRID(_ context.Context, apiName string) (string, error) {
	rid, ok := s.byAPIName[apiName]
	if !ok {
		return "", oms.ErrNotFound
	}
	return rid, nil
}

type stubExecutor struct {
	mu       sync.Mutex
	called   []string
	failOn   string
	failWith error
}

func (s *stubExecutor) Execute(_ context.Context, m modelmesh.ModelNode) error {
	s.mu.Lock()
	s.called = append(s.called, m.ID)
	s.mu.Unlock()
	if s.failOn != "" && m.ID == s.failOn {
		return s.failWith
	}
	return nil
}

func newRouter(t *testing.T, h *modelmesh.Handler) chi.Router {
	t.Helper()
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	return r
}

func postJSON(t *testing.T, r chi.Router, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHandler_Given_LinearChain_When_PostPlan_Then_ReturnsLayers(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"main": "ri.oms.main.ontology.x"}}
	h := modelmesh.NewHandler(resolver, nil)
	router := newRouter(t, h)

	body := map[string]any{
		"models": []map[string]any{
			{"id": "m2", "inputProperties": []string{"A"}, "outputProperties": []string{"B"}},
			{"id": "m1", "outputProperties": []string{"A"}},
			{"id": "m3", "inputProperties": []string{"B"}},
		},
	}
	w := postJSON(t, router, "/api/vertex/v1/ontologies/main/model-mesh/plan", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Layers [][]string `json:"layers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(resp.Layers) != 3 {
		t.Fatalf("expected 3 layers, got %v", resp.Layers)
	}
	if resp.Layers[0][0] != "m1" || resp.Layers[1][0] != "m2" || resp.Layers[2][0] != "m3" {
		t.Fatalf("unexpected layer order: %v", resp.Layers)
	}
}

func TestHandler_Given_Cycle_When_PostPlan_Then_400CycleDetected(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"main": "ri.oms.main.ontology.x"}}
	h := modelmesh.NewHandler(resolver, nil)
	router := newRouter(t, h)

	body := map[string]any{
		"models": []map[string]any{
			{"id": "m1", "inputProperties": []string{"B"}, "outputProperties": []string{"A"}},
			{"id": "m2", "inputProperties": []string{"A"}, "outputProperties": []string{"B"}},
		},
	}
	w := postJSON(t, router, "/api/vertex/v1/ontologies/main/model-mesh/plan", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	var env struct {
		ErrorName  string            `json:"errorName"`
		ErrorCode  string            `json:"errorCode"`
		Parameters map[string]string `json:"parameters"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v body=%s", err, w.Body.String())
	}
	if env.ErrorName != "CycleDetected" {
		t.Fatalf("expected errorName=CycleDetected, got %q (body=%s)", env.ErrorName, w.Body.String())
	}
	cycle := env.Parameters["cycle"]
	if cycle == "" || !strings.Contains(cycle, "m1") || !strings.Contains(cycle, "m2") {
		t.Fatalf("expected cycle parameter to mention m1+m2, got %q", cycle)
	}
}

func TestHandler_Given_Cycle_When_PostRun_Then_400CycleDetected(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"main": "ri.oms.main.ontology.x"}}
	exec := &stubExecutor{}
	h := modelmesh.NewHandler(resolver, exec.Execute)
	router := newRouter(t, h)

	body := map[string]any{
		"models": []map[string]any{
			{"id": "m1", "inputProperties": []string{"B"}, "outputProperties": []string{"A"}},
			{"id": "m2", "inputProperties": []string{"A"}, "outputProperties": []string{"B"}},
		},
	}
	w := postJSON(t, router, "/api/vertex/v1/ontologies/main/model-mesh/run", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if len(exec.called) != 0 {
		t.Fatalf("expected no executor calls on cycle, got %v", exec.called)
	}
}

func TestHandler_Given_ValidMesh_When_PostRun_Then_ResultsReturnedInTopoOrder(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"main": "ri.oms.main.ontology.x"}}
	exec := &stubExecutor{}
	h := modelmesh.NewHandler(resolver, exec.Execute)
	router := newRouter(t, h)

	body := map[string]any{
		"models": []map[string]any{
			{"id": "m2", "inputProperties": []string{"A"}, "outputProperties": []string{"B"}},
			{"id": "m1", "outputProperties": []string{"A"}},
		},
	}
	w := postJSON(t, router, "/api/vertex/v1/ontologies/main/model-mesh/run", body)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Layers  [][]string `json:"layers"`
		Results []struct {
			ModelID string `json:"modelId"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v body=%s", err, w.Body.String())
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %v", resp.Results)
	}
	if exec.called[0] != "m1" || exec.called[1] != "m2" {
		t.Fatalf("expected exec order [m1 m2], got %v", exec.called)
	}
}

func TestHandler_Given_UnknownOntology_When_PostPlan_Then_404(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{}}
	h := modelmesh.NewHandler(resolver, nil)
	router := newRouter(t, h)

	body := map[string]any{
		"models": []map[string]any{{"id": "m1"}},
	}
	w := postJSON(t, router, "/api/vertex/v1/ontologies/missing/model-mesh/plan", body)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Given_MalformedJSON_When_PostPlan_Then_400(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"main": "ri.oms.main.ontology.x"}}
	h := modelmesh.NewHandler(resolver, nil)
	router := newRouter(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/vertex/v1/ontologies/main/model-mesh/plan", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Given_DuplicateModelIDs_When_PostPlan_Then_400(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"main": "ri.oms.main.ontology.x"}}
	h := modelmesh.NewHandler(resolver, nil)
	router := newRouter(t, h)

	body := map[string]any{
		"models": []map[string]any{
			{"id": "m1"},
			{"id": "m1"},
		},
	}
	w := postJSON(t, router, "/api/vertex/v1/ontologies/main/model-mesh/plan", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_Given_NoExecutor_When_PostRun_Then_500(t *testing.T) {
	resolver := &stubResolver{byAPIName: map[string]string{"main": "ri.oms.main.ontology.x"}}
	h := modelmesh.NewHandler(resolver, nil)
	router := newRouter(t, h)

	body := map[string]any{
		"models": []map[string]any{{"id": "m1"}},
	}
	w := postJSON(t, router, "/api/vertex/v1/ontologies/main/model-mesh/run", body)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}
