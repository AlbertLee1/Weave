package logic

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/liyang/weave/pkg/aip"
)

// ProviderResolver resolves an LLM provider name to the corresponding
// aip.Provider. The aip.Registry already implements this shape so the
// production wiring is one-line; tests can supply a hand-rolled fake.
type ProviderResolver interface {
	Get(name string) (aip.Provider, error)
}

// Tool is the minimal abstraction the `tool` node type dispatches into.
// Tools are stateless side-effect-free functions the executor can
// invoke during a flow run; concrete tools live in tool.go.
type Tool interface {
	Name() string
	Invoke(ctx context.Context, params map[string]any) (map[string]any, error)
}

// ToolRegistry is the lookup surface for tools. Mirrors the LLM
// provider Registry — keyed lookup by name with an explicit
// "not found" sentinel.
type ToolRegistry interface {
	Lookup(name string) (Tool, bool)
}

// ErrToolNotFound is returned by the tool node when ToolRegistry.Lookup
// fails. The handler maps it to a structured 500 with the missing tool
// name in parameters.
var ErrToolNotFound = errors.New("aip-logic: tool not registered")

// Executor walks a Flow's DAG, dispatching each node into the matching
// runtime (LLM provider for `llm`, tool registry for `tool`, in-process
// evaluation for `if` / `output`). Per-node output is captured into a
// shared state map keyed by node ID so downstream nodes can reference
// upstream outputs via {{nodeId.fieldName}} placeholders.
type Executor struct {
	Providers ProviderResolver
	Tools     ToolRegistry
}

// NewExecutor returns an Executor wired to the given provider and tool
// registries. Either may be nil — running a flow without an LLM or tool
// node is supported and the missing-resolver only matters when the flow
// actually contains the corresponding node type.
func NewExecutor(p ProviderResolver, t ToolRegistry) *Executor {
	return &Executor{Providers: p, Tools: t}
}

// Execute runs flow with the given input map. The returned Run is fully
// populated (status / output / trace / error). Validation errors are
// surfaced via err AND captured on Run.Error so callers may persist the
// run row even on early failure.
func (e *Executor) Execute(ctx context.Context, flow *Flow, input map[string]any) (*Run, error) {
	if flow == nil {
		return nil, errors.New("flow is nil")
	}
	if err := flow.Validate(); err != nil {
		return &Run{FlowID: flow.ID, Status: RunStatusFailed, Error: err.Error()}, err
	}
	order, err := flow.TopoOrder()
	if err != nil {
		return &Run{FlowID: flow.ID, Status: RunStatusFailed, Error: err.Error()}, err
	}

	state := map[string]any{}
	if input != nil {
		state["input"] = input
	}
	skipped := map[string]bool{}
	edgeActive := make([]bool, len(flow.Edges))
	for i := range edgeActive {
		edgeActive[i] = true
	}
	incomingByNode := buildIncomingEdges(flow.Edges)
	outgoingByNode := buildOutgoingEdges(flow.Edges)
	trace := make([]TraceEntry, 0, len(order))
	nodesByID := indexNodes(flow.Nodes)

	for _, id := range order {
		node := nodesByID[id]
		if !nodeReachable(node.ID, incomingByNode, edgeActive) {
			skipped[node.ID] = true
			trace = append(trace, TraceEntry{NodeID: node.ID, Type: node.Type, Status: TraceStatusSkipped})
			// Disable outgoing edges so dead branches propagate.
			for _, idx := range outgoingByNode[node.ID] {
				edgeActive[idx] = false
			}
			continue
		}
		entry, branchTaken, err := e.runNode(ctx, node, state, input)
		trace = append(trace, entry)
		if err != nil {
			run := &Run{
				FlowID: flow.ID,
				Status: RunStatusFailed,
				Input:  input,
				Output: state,
				Trace:  trace,
				Error:  err.Error(),
			}
			return run, err
		}
		if entry.Output != nil {
			state[node.ID] = entry.Output
		}
		// For if-nodes, deactivate outgoing edges that label a different
		// branch than the one taken. Unlabelled edges (Branch == "") on
		// an if-node are unconditional — fan out to every downstream.
		if node.Type == NodeTypeIf {
			for _, idx := range outgoingByNode[node.ID] {
				e := flow.Edges[idx]
				if e.Branch != "" && e.Branch != branchTaken {
					edgeActive[idx] = false
				}
			}
		}
	}

	output := collectOutputs(flow, state)
	return &Run{
		FlowID: flow.ID,
		Status: RunStatusSuccess,
		Input:  input,
		Output: output,
		Trace:  trace,
	}, nil
}

// runNode dispatches one node to its concrete handler and returns the
// trace entry, the if-branch taken (or ""), and any execution error.
func (e *Executor) runNode(ctx context.Context, n Node, state, input map[string]any) (TraceEntry, string, error) {
	entry := TraceEntry{NodeID: n.ID, Type: n.Type, Status: TraceStatusSuccess}
	switch n.Type {
	case NodeTypeLLM:
		out, err := e.runLLMNode(ctx, n, state)
		if err != nil {
			entry.Status = TraceStatusFailed
			entry.Error = err.Error()
			return entry, "", err
		}
		entry.Output = out
		return entry, "", nil
	case NodeTypeTool:
		out, err := e.runToolNode(ctx, n, state)
		if err != nil {
			entry.Status = TraceStatusFailed
			entry.Error = err.Error()
			return entry, "", err
		}
		entry.Output = out
		return entry, "", nil
	case NodeTypeIf:
		branch, err := runIfNode(n, state)
		if err != nil {
			entry.Status = TraceStatusFailed
			entry.Error = err.Error()
			return entry, "", err
		}
		entry.Output = map[string]any{"branch": branch}
		return entry, branch, nil
	case NodeTypeOutput:
		out := runOutputNode(n, state, input)
		entry.Output = out
		return entry, "", nil
	default:
		err := fmt.Errorf("unknown node type %q", n.Type)
		entry.Status = TraceStatusFailed
		entry.Error = err.Error()
		return entry, "", err
	}
}

func (e *Executor) runLLMNode(ctx context.Context, n Node, state map[string]any) (map[string]any, error) {
	if e.Providers == nil {
		return nil, errors.New("aip-logic: no provider registry wired")
	}
	providerName := cfgString(n.Config, "provider")
	prov, err := e.Providers.Get(providerName)
	if err != nil {
		return nil, fmt.Errorf("provider lookup failed: %w", err)
	}
	prompt := substituteVars(cfgString(n.Config, "promptTemplate"), state)
	if prompt == "" {
		prompt = substituteVars(cfgString(n.Config, "prompt"), state)
	}
	systemPrompt := substituteVars(cfgString(n.Config, "systemPrompt"), state)
	model := cfgString(n.Config, "model")

	msgs := make([]aip.ChatMessage, 0, 2)
	if systemPrompt != "" {
		msgs = append(msgs, aip.ChatMessage{Role: aip.RoleSystem, Content: systemPrompt})
	}
	msgs = append(msgs, aip.ChatMessage{Role: aip.RoleUser, Content: prompt})

	resp, err := prov.Complete(ctx, aip.ChatRequest{Model: model, Messages: msgs})
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"content":    resp.Content,
		"model":      resp.Model,
		"tokenCount": resp.TokenCount,
	}
	return out, nil
}

func (e *Executor) runToolNode(ctx context.Context, n Node, state map[string]any) (map[string]any, error) {
	if e.Tools == nil {
		return nil, ErrToolNotFound
	}
	toolName := cfgString(n.Config, "tool")
	tool, ok := e.Tools.Lookup(toolName)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrToolNotFound, toolName)
	}
	rawParams, _ := n.Config["params"].(map[string]any)
	params := substituteVarsMap(rawParams, state)
	out, err := tool.Invoke(ctx, params)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

// runIfNode evaluates a small expression-shaped condition against the
// current state. The condition format is a tiny three-token
// `<lhs> <op> <rhs>` mini-DSL where lhs may reference state via
// {{node.field}}, op is one of ==, !=, <, <=, >, >=, contains, and rhs
// is either a quoted string, a number, true/false, or another
// {{node.field}} reference.
func runIfNode(n Node, state map[string]any) (string, error) {
	cond := cfgString(n.Config, "condition")
	cond = strings.TrimSpace(cond)
	if cond == "" {
		return "", errors.New("if node missing condition")
	}
	// Substitute {{...}} placeholders with their JSON string forms.
	cond = substituteVars(cond, state)
	result, err := evalCondition(cond)
	if err != nil {
		return "", err
	}
	if result {
		return BranchTrue, nil
	}
	return BranchFalse, nil
}

// runOutputNode collects the configured keys from state into a flat
// output map. When `keys` is omitted or empty, the full state map is
// returned (minus the reserved "input" key which the caller already has).
func runOutputNode(n Node, state, input map[string]any) map[string]any {
	rawKeys, _ := n.Config["keys"].([]any)
	if len(rawKeys) == 0 {
		out := make(map[string]any, len(state))
		for k, v := range state {
			if k == "input" {
				continue
			}
			out[k] = v
		}
		_ = input
		return out
	}
	out := map[string]any{}
	for _, k := range rawKeys {
		ks, _ := k.(string)
		if ks == "" {
			continue
		}
		// Resolve dotted paths so callers can pull nested fields like
		// "n1.content" out of an llm node's output map.
		val, ok := lookupPath(state, ks)
		if !ok {
			continue
		}
		out[ks] = val
	}
	return out
}

// collectOutputs walks output-typed nodes in the flow and builds the
// final returned-to-caller payload. Multiple output nodes are merged in
// declaration order (later wins on key collision).
func collectOutputs(flow *Flow, state map[string]any) map[string]any {
	out := map[string]any{}
	hasOutput := false
	for _, n := range flow.Nodes {
		if n.Type != NodeTypeOutput {
			continue
		}
		hasOutput = true
		v, ok := state[n.ID].(map[string]any)
		if !ok {
			continue
		}
		for k, vv := range v {
			out[k] = vv
		}
	}
	if !hasOutput {
		// Default: return every node's output keyed by node id.
		for k, v := range state {
			if k == "input" {
				continue
			}
			out[k] = v
		}
	}
	return out
}

func indexNodes(nodes []Node) map[string]Node {
	out := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		out[n.ID] = n
	}
	return out
}

// buildOutgoingEdges maps node IDs to the indices of edges whose From
// matches the node. Indices index into the flow.Edges slice so callers
// can mutate per-edge state (e.g. the active flag) directly.
func buildOutgoingEdges(edges []Edge) map[string][]int {
	out := map[string][]int{}
	for i, e := range edges {
		out[e.From] = append(out[e.From], i)
	}
	return out
}

// buildIncomingEdges is buildOutgoingEdges' counterpart, keyed by To.
func buildIncomingEdges(edges []Edge) map[string][]int {
	out := map[string][]int{}
	for i, e := range edges {
		out[e.To] = append(out[e.To], i)
	}
	return out
}

// nodeReachable reports whether the node should run. A node with no
// incoming edges (root) is always reachable; otherwise the node is
// reachable iff at least one incoming edge is still marked active.
func nodeReachable(id string, incoming map[string][]int, edgeActive []bool) bool {
	idxs, ok := incoming[id]
	if !ok || len(idxs) == 0 {
		return true
	}
	for _, idx := range idxs {
		if edgeActive[idx] {
			return true
		}
	}
	return false
}
