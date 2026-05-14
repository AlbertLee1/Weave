package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type stubVertex struct {
	graphs     []VertexGraphSummary
	runResult  *VertexRunResult
	runErr     error
	applyResult *VertexApplyResult
	calledList  string
	calledRun   string
	calledApply string
}

func (s *stubVertex) ListGraphs(_ context.Context, ontologyRID string) ([]VertexGraphSummary, error) {
	s.calledList = ontologyRID
	return s.graphs, nil
}

func (s *stubVertex) RunScenario(_ context.Context, rid string) (*VertexRunResult, error) {
	s.calledRun = rid
	if s.runErr != nil {
		return nil, s.runErr
	}
	return s.runResult, nil
}

func (s *stubVertex) ApplyScenario(_ context.Context, rid string) (*VertexApplyResult, error) {
	s.calledApply = rid
	return s.applyResult, nil
}

func newServerWithVertex(t *testing.T, v VertexService) *Server {
	t.Helper()
	s := &Server{registry: NewRegistry()}
	s.SetVertexService(v)
	return s
}

func TestVertexTools_Given_RegisteredServer_When_ListedFromRegistry_Then_AllThreePresent(t *testing.T) {
	s := newServerWithVertex(t, &stubVertex{})
	have := map[string]bool{}
	for _, d := range s.Registry().List() {
		have[d.Name] = true
	}
	for _, name := range []string{"vertex_list_graphs", "vertex_run_scenario", "vertex_apply_scenario"} {
		if !have[name] {
			t.Errorf("missing tool %q", name)
		}
	}
}

func TestVertexListGraphs_Given_StubVertex_When_Called_Then_DelegatesAndReturnsJSON(t *testing.T) {
	v := &stubVertex{graphs: []VertexGraphSummary{{RID: "ri.vertex.main.graph.a", Name: "alpha"}}}
	s := newServerWithVertex(t, v)
	tool, _ := s.Registry().Get("vertex_list_graphs")
	res, err := tool.Call(context.Background(), map[string]any{"ontologyRid": "ri.weave.main.ontology.aviation"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if v.calledList != "ri.weave.main.ontology.aviation" {
		t.Errorf("ListGraphs called with %q", v.calledList)
	}
	if len(res.Content) != 1 || !strings.Contains(res.Content[0].Text, "alpha") {
		t.Errorf("content = %+v", res.Content)
	}
	var got []VertexGraphSummary
	if err := json.Unmarshal([]byte(res.Content[0].Text), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 || got[0].Name != "alpha" {
		t.Errorf("decoded = %+v", got)
	}
}

func TestVertexRunScenario_Given_StubVertex_When_Called_Then_ReturnsTerminalRecord(t *testing.T) {
	v := &stubVertex{runResult: &VertexRunResult{ScenarioRunRID: "r1", Status: "succeeded", DurationMs: 42}}
	s := newServerWithVertex(t, v)
	tool, _ := s.Registry().Get("vertex_run_scenario")
	res, err := tool.Call(context.Background(), map[string]any{"scenarioRid": "ri.vertex.main.scenario.s1"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if v.calledRun != "ri.vertex.main.scenario.s1" {
		t.Errorf("RunScenario called with %q", v.calledRun)
	}
	if !strings.Contains(res.Content[0].Text, "succeeded") {
		t.Errorf("output = %q", res.Content[0].Text)
	}
}

func TestVertexRunScenario_Given_VertexErrors_When_Called_Then_PropagatesError(t *testing.T) {
	v := &stubVertex{runErr: errors.New("boom")}
	s := newServerWithVertex(t, v)
	tool, _ := s.Registry().Get("vertex_run_scenario")
	_, err := tool.Call(context.Background(), map[string]any{"scenarioRid": "ri.vertex.main.scenario.s1"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected boom error; got %v", err)
	}
}

func TestVertexApplyScenario_Given_StubVertex_When_Called_Then_ReturnsOntologyCommit(t *testing.T) {
	v := &stubVertex{applyResult: &VertexApplyResult{OntologyCommit: "commit-B"}}
	s := newServerWithVertex(t, v)
	tool, _ := s.Registry().Get("vertex_apply_scenario")
	res, err := tool.Call(context.Background(), map[string]any{"scenarioRid": "ri.vertex.main.scenario.s1"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !strings.Contains(res.Content[0].Text, "commit-B") {
		t.Errorf("output = %q", res.Content[0].Text)
	}
}

func TestVertexTools_Given_NoServiceConfigured_When_Called_Then_ReturnsClearError(t *testing.T) {
	s := &Server{registry: NewRegistry()}
	// Register tools without a backing service so we can drive the error path.
	registerVertexTools(s)
	for _, name := range []string{"vertex_list_graphs", "vertex_run_scenario", "vertex_apply_scenario"} {
		tool, _ := s.Registry().Get(name)
		args := map[string]any{}
		switch name {
		case "vertex_list_graphs":
			args["ontologyRid"] = "x"
		default:
			args["scenarioRid"] = "x"
		}
		_, err := tool.Call(context.Background(), args)
		if err == nil || !strings.Contains(err.Error(), "not configured") {
			t.Errorf("tool %s should error with 'not configured', got %v", name, err)
		}
	}
}
