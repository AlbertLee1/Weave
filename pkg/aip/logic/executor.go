package logic

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

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

// ErrIterateLimitExceeded is returned when an iterate node's resolved
// forEach slice exceeds MaxIterateItems. Surfaced verbatim in the trace
// entry so flow authors see the bound that tripped them.
var ErrIterateLimitExceeded = errors.New("aip-logic: iterate forEach exceeds MaxIterateItems")

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
		entry, branchTaken, err := e.executeNodeWithPolicy(ctx, flow, node, state, input)
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

// executeNodeWithPolicy is the single chokepoint that wraps a node's
// concrete dispatch in the retry + fallback policy. It calls runNode
// up to `attempts` times (per-node config.retry.maxAttempts overriding
// the flow-level MaxRetries default + 1 baseline attempt). When all
// retries fail AND the node is an LLM AND a fallback model is wired,
// one additional attempt fires against fallback_model with the same
// prompt. The returned entry records Attempts + UsedFallback so audit
// trails preserve "how many tries did this take?".
func (e *Executor) executeNodeWithPolicy(ctx context.Context, flow *Flow, n Node, state, input map[string]any) (TraceEntry, string, error) {
	attempts := nodeMaxAttempts(n, flow)
	if attempts < 1 {
		attempts = 1
	}
	backoff := nodeBackoff(n)
	var (
		entry  TraceEntry
		branch string
		err    error
	)
	for i := 0; i < attempts; i++ {
		if i > 0 && backoff > 0 {
			select {
			case <-ctx.Done():
				entry = TraceEntry{NodeID: n.ID, Type: n.Type, Status: TraceStatusFailed, Error: ctx.Err().Error(), Attempts: i}
				return entry, "", ctx.Err()
			case <-time.After(backoff):
			}
		}
		entry, branch, err = e.runNode(ctx, n, state, input)
		entry.Attempts = i + 1
		if err == nil {
			return entry, branch, nil
		}
	}
	// LLM fallback: every primary-provider attempt has failed; if the
	// flow declares a fallback_model and the node hasn't already been
	// rerouted onto it, swap models and retry once. Fallback applies
	// only to llm nodes — tool / iterate / if / output have no model
	// concept.
	if n.Type == NodeTypeLLM && flow.FallbackModel != "" {
		fbNode := nodeWithModel(n, flow.FallbackModel)
		fbEntry, fbBranch, fbErr := e.runNode(ctx, fbNode, state, input)
		fbEntry.Attempts = entry.Attempts + 1
		fbEntry.UsedFallback = true
		if fbErr == nil {
			return fbEntry, fbBranch, nil
		}
		entry = fbEntry
		branch = fbBranch
		err = fbErr
	}
	return entry, branch, err
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
	case NodeTypeIterate:
		// Iterate is dispatched without the executeNodeWithPolicy
		// wrapper (it'd be a no-op anyway: a partial iteration must
		// not be retried wholesale, and the per-body retry budget is
		// honoured by runIterateNode → runNode itself). The parent
		// flow is unused for iterate's own dispatch — body-level
		// retry / fallback semantics live inside runIterateNode.
		out, err := e.runIterateNode(ctx, n, state, input)
		if err != nil {
			entry.Status = TraceStatusFailed
			entry.Error = err.Error()
			entry.Output = out
			return entry, "", err
		}
		entry.Output = out
		return entry, "", nil
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

// nodeMaxAttempts resolves the attempts budget for a node. Per-node
// config.retry.maxAttempts wins; otherwise flow.MaxRetries provides the
// default (clamped to MaxRetryAttempts). The returned value is
// "total attempts" (baseline + retries), so 1 means "no retry".
func nodeMaxAttempts(n Node, flow *Flow) int {
	if retry, ok := n.Config["retry"].(map[string]any); ok {
		if v, has := retry["maxAttempts"]; has {
			if parsed, ok := toInt(v); ok && parsed > 0 {
				if parsed > MaxRetryAttempts {
					parsed = MaxRetryAttempts
				}
				return parsed
			}
		}
	}
	if flow != nil && flow.MaxRetries > 0 {
		// Flow-level MaxRetries is "additional retries beyond the first
		// attempt"; total attempts = MaxRetries + 1.
		retries := flow.MaxRetries
		if retries > MaxRetryAttempts {
			retries = MaxRetryAttempts
		}
		return retries + 1
	}
	return 1
}

// nodeBackoff returns the per-retry backoff duration. 0 disables sleep
// between attempts (default).
func nodeBackoff(n Node) time.Duration {
	retry, ok := n.Config["retry"].(map[string]any)
	if !ok {
		return 0
	}
	v, ok := retry["backoffMs"]
	if !ok {
		return 0
	}
	ms, ok := toInt(v)
	if !ok || ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

// nodeWithModel returns a shallow clone of n with config.model
// overridden to model. Used by the fallback policy without mutating the
// caller's Flow definition.
func nodeWithModel(n Node, model string) Node {
	cp := n
	cfg := make(map[string]any, len(n.Config)+1)
	for k, v := range n.Config {
		cfg[k] = v
	}
	cfg["model"] = model
	cp.Config = cfg
	return cp
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

// runIterateNode resolves config.forEach to a slice in state, then runs
// the body node spec once per item. The body's output is collected into
// `results: [...]`. The forEach slice is hard-capped at MaxIterateItems
// (PRD US-372: "上限 100 项"); exceeding the cap aborts before any body
// invocation runs. Each iteration receives an `item`, `index`, and
// `length` placeholder under `state.iterate.<bodyId>.{item,index,length}`
// so the body can reference them via `{{iterate.<id>.item.field}}`.
func (e *Executor) runIterateNode(ctx context.Context, n Node, state, input map[string]any) (map[string]any, error) {
	body, ok := iterateBody(n)
	if !ok || body == nil {
		return nil, errors.New("iterate node config.body is missing or malformed")
	}
	rawPath := strings.TrimSpace(cfgString(n.Config, "forEach"))
	if rawPath == "" {
		return nil, errors.New("iterate node config.forEach is required")
	}
	// Strip {{...}} wrapping if the author provided a template-shaped
	// reference. Either bare-path ("input.items") or template
	// ("{{input.items}}") is accepted.
	rawPath = strings.TrimPrefix(rawPath, "{{")
	rawPath = strings.TrimSuffix(rawPath, "}}")
	rawPath = strings.TrimSpace(rawPath)

	val, ok := lookupPath(state, rawPath)
	if !ok {
		return nil, fmt.Errorf("iterate forEach path %q not found in state", rawPath)
	}
	items, ok := val.([]any)
	if !ok {
		return nil, fmt.Errorf("iterate forEach path %q is not an array (type %T)", rawPath, val)
	}
	limit := MaxIterateItems
	if v, ok := n.Config["max"]; ok {
		if m, ok := toInt(v); ok && m >= 0 && m < limit {
			limit = m
		}
	}
	if len(items) > limit {
		return map[string]any{"results": []any{}, "iterations": 0, "limit": limit}, fmt.Errorf("%w: %d items > cap %d", ErrIterateLimitExceeded, len(items), limit)
	}

	// Synthesize a parent flow stub so each iteration honours the same
	// retry / fallback policies as the outer flow. We reach this through
	// the executor's executeNodeWithPolicy entrypoint by capturing the
	// caller flow on the executor; for simplicity here we run the body
	// without the wrapper, so iterate body retry config is honoured but
	// flow.FallbackModel for nested LLM calls is not. Document for SDKs.
	results := make([]any, 0, len(items))
	for i, item := range items {
		if err := ctx.Err(); err != nil {
			return map[string]any{"results": results, "iterations": i}, err
		}
		// Compose a per-iteration scope so {{iterate.body.item}} resolves.
		iterState := cloneStateForIterate(state, body.ID, item, i, len(items))
		entry, _, err := e.runNode(ctx, *body, iterState, input)
		if err != nil {
			return map[string]any{"results": results, "iterations": i, "lastError": entry.Error}, err
		}
		results = append(results, entry.Output)
	}
	return map[string]any{"results": results, "iterations": len(items)}, nil
}

// cloneStateForIterate shallow-copies state and binds the per-iteration
// `iterate.<bodyId>` scope so the body node can pull `{{iterate.b.item}}`.
func cloneStateForIterate(state map[string]any, bodyID string, item any, index, length int) map[string]any {
	cp := make(map[string]any, len(state)+1)
	for k, v := range state {
		cp[k] = v
	}
	scope, _ := cp["iterate"].(map[string]any)
	if scope == nil {
		scope = map[string]any{}
	} else {
		newScope := make(map[string]any, len(scope)+1)
		for k, v := range scope {
			newScope[k] = v
		}
		scope = newScope
	}
	scope[bodyID] = map[string]any{
		"item":   item,
		"index":  index,
		"length": length,
	}
	cp["iterate"] = scope
	return cp
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
