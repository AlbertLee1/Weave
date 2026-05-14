// VTX-112 — Vertex MCP tools.
//
// Three tools (vertex_list_graphs, vertex_run_scenario,
// vertex_apply_scenario) expose the Vertex surface to Claude / other MCP
// clients. Each tool delegates to a VertexService injected by the host
// (cmd/server boot wires it; tests inject a stub). Tools are
// registration-idempotent so SetVertexService can be called more than
// once without panicking.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
)

// VertexService is the surface MCP tools call into. Implementations are
// expected to wrap the same underlying Scenario handlers used by HTTP
// (VTX-044) so a tool call lands in the same code path as the
// equivalent REST request.
type VertexService interface {
	ListGraphs(ctx context.Context, ontologyRID string) ([]VertexGraphSummary, error)
	RunScenario(ctx context.Context, scenarioRID string) (*VertexRunResult, error)
	ApplyScenario(ctx context.Context, scenarioRID string) (*VertexApplyResult, error)
}

// VertexGraphSummary mirrors the wire shape returned by /api/vertex/v1/graphs.
type VertexGraphSummary struct {
	RID  string `json:"rid"`
	Name string `json:"name"`
}

// VertexRunResult is the terminal Run record (status + scenarioRunRid +
// durationMs). Mirrors the SDK shape.
type VertexRunResult struct {
	ScenarioRunRID string `json:"scenarioRunRid"`
	Status         string `json:"status"`
	DurationMs     int64  `json:"durationMs"`
}

// VertexApplyResult is the response from /apply.
type VertexApplyResult struct {
	OntologyCommit string `json:"ontologyCommit"`
}

// SetVertexService wires the Vertex tools into the registry. Idempotent.
func (s *Server) SetVertexService(v VertexService) {
	s.vertexService = v
	registerVertexTools(s)
}

func registerVertexTools(s *Server) {
	tools := []Tool{
		&vertexListGraphsTool{s: s},
		&vertexRunScenarioTool{s: s},
		&vertexApplyScenarioTool{s: s},
	}
	for _, t := range tools {
		if _, ok := s.registry.Get(t.Definition().Name); ok {
			continue
		}
		if err := s.registry.Register(t); err != nil {
			panic(fmt.Sprintf("mcp: register vertex tool: %v", err))
		}
	}
}

// ---------------------------------------------------------------------------
// vertex_list_graphs
// ---------------------------------------------------------------------------

type vertexListGraphsTool struct{ s *Server }

func (t *vertexListGraphsTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "vertex_list_graphs",
		Description: "List Vertex System Graphs within an ontology.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"ontologyRid": {Type: "string", Description: "Ontology RID or apiName."},
			},
			Required: []string{"ontologyRid"},
		},
	}
}

func (t *vertexListGraphsTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.vertexService == nil {
		return nil, fmt.Errorf("vertex service not configured")
	}
	ont, err := stringArg(args, "ontologyRid")
	if err != nil {
		return nil, err
	}
	out, err := t.s.vertexService.ListGraphs(ctx, ont)
	if err != nil {
		return nil, err
	}
	return jsonResult(out)
}

// ---------------------------------------------------------------------------
// vertex_run_scenario
// ---------------------------------------------------------------------------

type vertexRunScenarioTool struct{ s *Server }

func (t *vertexRunScenarioTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "vertex_run_scenario",
		Description: "Run a Vertex Scenario and return its terminal status. Does not stream — for SSE, use the HTTP /run endpoint.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"scenarioRid": {Type: "string", Description: "Scenario RID to run."},
			},
			Required: []string{"scenarioRid"},
		},
	}
}

func (t *vertexRunScenarioTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.vertexService == nil {
		return nil, fmt.Errorf("vertex service not configured")
	}
	rid, err := stringArg(args, "scenarioRid")
	if err != nil {
		return nil, err
	}
	out, err := t.s.vertexService.RunScenario(ctx, rid)
	if err != nil {
		return nil, err
	}
	return jsonResult(out)
}

// ---------------------------------------------------------------------------
// vertex_apply_scenario
// ---------------------------------------------------------------------------

type vertexApplyScenarioTool struct{ s *Server }

func (t *vertexApplyScenarioTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "vertex_apply_scenario",
		Description: "Fold a Vertex Scenario back into the main ontology and return the new ontology commit.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]PropertySchema{
				"scenarioRid": {Type: "string", Description: "Scenario RID to apply."},
			},
			Required: []string{"scenarioRid"},
		},
	}
}

func (t *vertexApplyScenarioTool) Call(ctx context.Context, args map[string]any) (*ToolResult, error) {
	if t.s.vertexService == nil {
		return nil, fmt.Errorf("vertex service not configured")
	}
	rid, err := stringArg(args, "scenarioRid")
	if err != nil {
		return nil, err
	}
	out, err := t.s.vertexService.ApplyScenario(ctx, rid)
	if err != nil {
		return nil, err
	}
	return jsonResult(out)
}

// jsonResult is a small helper — every Vertex tool's output is JSON; the
// MCP wire layer wraps it as a text content block.
func jsonResult(v any) (*ToolResult, error) {
	buf, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return &ToolResult{Content: []Content{{Type: "text", Text: string(buf)}}}, nil
}
