// Package logic implements the persistent AIP Logic Flow service
// (US-281). A Flow is a DAG of nodes the executor walks in topological
// order; each node is one of llm | tool | if | output. The package is
// intentionally split from the parent pkg/aip thread package so the
// flow types stay independently versionable and the executor can take
// a tiny dependency on aip.Provider / aip.Registry without dragging in
// the whole conversation surface.
package logic

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

// Node type constants. Matches the JSON wire shape used by the SPA
// editor and persisted as `nodes[*].type` in the aip_logic_flows table.
const (
	NodeTypeLLM     = "llm"
	NodeTypeTool    = "tool"
	NodeTypeIf      = "if"
	NodeTypeIterate = "iterate"
	NodeTypeOutput  = "output"
)

// MaxIterateItems caps the forEach iteration count for an iterate node
// (US-372 acceptance gate: "上限 100 项"). Hardcoded rather than
// configurable so a malformed flow cannot bypass the bound.
const MaxIterateItems = 100

// MaxRetryAttempts caps the per-node retry budget (so a single misfit
// flow cannot pin a worker for hours). Mirrors the migration check
// constraint on aip_logic_flows.max_retries.
const MaxRetryAttempts = 8

// IsKnownNodeType reports whether name is a built-in node type.
func IsKnownNodeType(name string) bool {
	switch name {
	case NodeTypeLLM, NodeTypeTool, NodeTypeIf, NodeTypeIterate, NodeTypeOutput:
		return true
	}
	return false
}

// KnownNodeTypes returns the canonical list in stable order.
func KnownNodeTypes() []string {
	return []string{NodeTypeLLM, NodeTypeTool, NodeTypeIf, NodeTypeIterate, NodeTypeOutput}
}

// Flow is one persisted Logic Flow row. Nodes carry the executable
// configuration; Edges describe data flow between nodes. FallbackModel
// and MaxRetries are flow-level defaults the executor consults when an
// individual node does not pin its own. They mirror the
// fallback_model / max_retries columns on aip_logic_flows.
type Flow struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	Nodes         []Node    `json:"nodes"`
	Edges         []Edge    `json:"edges"`
	FallbackModel string    `json:"fallbackModel,omitempty"`
	MaxRetries    int       `json:"maxRetries,omitempty"`
	CreatedBy     string    `json:"createdBy,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// FlowUpdate is the partial-update payload. Pointer fields preserve
// "omit=keep current" semantics.
type FlowUpdate struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	Nodes         *[]Node `json:"nodes,omitempty"`
	Edges         *[]Edge `json:"edges,omitempty"`
	FallbackModel *string `json:"fallbackModel,omitempty"`
	MaxRetries    *int    `json:"maxRetries,omitempty"`
}

// Node is one executable step. Config is a free-form JSON object whose
// schema is validated per node type at execute time.
type Node struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

// Edge connects two nodes. When the source is an `if` node the edge's
// Branch field selects which side of the condition the edge belongs to
// ("true" / "false"); for other node types Branch is ignored.
type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Branch string `json:"branch,omitempty"`
}

// Run is one captured execution of a Flow. The store persists Run rows
// for audit; the executor returns the same shape directly to API
// callers so /execute and /runs return identically structured payloads.
type Run struct {
	ID        int64          `json:"id"`
	FlowID    string         `json:"flowId"`
	Status    string         `json:"status"`
	Input     map[string]any `json:"input,omitempty"`
	Output    map[string]any `json:"output,omitempty"`
	Trace     []TraceEntry   `json:"trace,omitempty"`
	Error     string         `json:"error,omitempty"`
	CreatedBy string         `json:"createdBy,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

// TraceEntry is one step record from a Run. Status is one of
// "success", "skipped", or "failed". Attempts is the number of dispatch
// attempts the executor made before recording this entry (1 when no
// retry occurred); UsedFallback is true when the LLM fallback model
// was substituted for the node's primary provider. UsedFallbackNode +
// FallbackNodeID are set (US-478) when a node-level config.fallbackNodeId
// rerouted execution to a sibling node after the primary retry budget
// was exhausted — they are recorded whether the fallback target itself
// succeeded or also failed, so post-hoc audits see which alternative was
// tried.
type TraceEntry struct {
	NodeID           string         `json:"nodeId"`
	Type             string         `json:"type"`
	Status           string         `json:"status"`
	Output           map[string]any `json:"output,omitempty"`
	Error            string         `json:"error,omitempty"`
	Attempts         int            `json:"attempts,omitempty"`
	UsedFallback     bool           `json:"usedFallback,omitempty"`
	UsedFallbackNode bool           `json:"usedFallbackNode,omitempty"`
	FallbackNodeID   string         `json:"fallbackNodeId,omitempty"`
}

// Run status constants.
const (
	RunStatusSuccess = "success"
	RunStatusFailed  = "failed"
)

// Trace status constants.
const (
	TraceStatusSuccess = "success"
	TraceStatusSkipped = "skipped"
	TraceStatusFailed  = "failed"
)

// Edge branch constants. "" is the default (unconditional) branch.
const (
	BranchTrue  = "true"
	BranchFalse = "false"
)

// flowIDRE matches the canonical flow identifier shape; mirrors the
// allowlist used by aip_threads / feature_flags.
var flowIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// ValidateFlowID rejects empty / over-long / disallowed-character ids.
func ValidateFlowID(id string) error {
	if id == "" {
		return errors.New("flow id must not be empty")
	}
	if !flowIDRE.MatchString(id) {
		return fmt.Errorf("flow id %q is invalid: allowed characters are [A-Za-z0-9._-] and length must be 1..128", id)
	}
	return nil
}

// Validate checks structural invariants on the flow:
//   - at least one node
//   - all node IDs are non-empty + unique
//   - every node type is known
//   - per-type Config minima (llm needs provider; if needs condition;
//     tool needs tool name)
//   - every edge references known nodes
//   - the graph is acyclic (TopoOrder succeeds)
func (f *Flow) Validate() error {
	if f == nil {
		return errors.New("flow is nil")
	}
	if err := ValidateFlowID(f.ID); err != nil {
		return err
	}
	if len(f.Nodes) == 0 {
		return errors.New("flow must contain at least one node")
	}
	if f.MaxRetries < 0 || f.MaxRetries > MaxRetryAttempts {
		return fmt.Errorf("flow maxRetries must be in [0, %d]", MaxRetryAttempts)
	}
	seen := make(map[string]struct{}, len(f.Nodes))
	for i, n := range f.Nodes {
		if n.ID == "" {
			return fmt.Errorf("node[%d] id must not be empty", i)
		}
		if !flowIDRE.MatchString(n.ID) {
			return fmt.Errorf("node[%d] id %q is invalid", i, n.ID)
		}
		if _, dup := seen[n.ID]; dup {
			return fmt.Errorf("duplicate node id %q", n.ID)
		}
		seen[n.ID] = struct{}{}
		if !IsKnownNodeType(n.Type) {
			return fmt.Errorf("node %q has unknown type %q", n.ID, n.Type)
		}
		if err := validateNodeConfig(n); err != nil {
			return fmt.Errorf("node %q: %w", n.ID, err)
		}
	}
	for i, e := range f.Edges {
		if _, ok := seen[e.From]; !ok {
			return fmt.Errorf("edge[%d] from %q is not a known node", i, e.From)
		}
		if _, ok := seen[e.To]; !ok {
			return fmt.Errorf("edge[%d] to %q is not a known node", i, e.To)
		}
		if e.From == e.To {
			return fmt.Errorf("edge[%d] is a self-loop on %q", i, e.From)
		}
	}
	// US-478 fallback target validation runs after the node-id set is
	// fully built so cross-references resolve regardless of declaration
	// order.
	for _, n := range f.Nodes {
		fb := cfgString(n.Config, "fallbackNodeId")
		if fb == "" {
			continue
		}
		if _, ok := seen[fb]; !ok {
			return fmt.Errorf("node %q: fallbackNodeId %q is not a known node", n.ID, fb)
		}
		if fb == n.ID {
			return fmt.Errorf("node %q: fallbackNodeId must not reference the node itself", n.ID)
		}
	}
	if _, err := f.TopoOrder(); err != nil {
		return err
	}
	return nil
}

// TopoOrder returns node IDs in a topological order such that every
// edge goes from an earlier ID to a later ID. Returns an error if the
// graph contains a cycle.
func (f *Flow) TopoOrder() ([]string, error) {
	layers, err := f.TopoLayers()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(f.Nodes))
	for _, layer := range layers {
		out = append(out, layer...)
	}
	return out, nil
}

// TopoLayers returns node IDs grouped into topological layers: every
// node in layer[i] has all its dependencies in some earlier layer, so
// nodes within the same layer may be dispatched concurrently (US-478).
// Layers preserve declaration order as a tie-breaker so each layer's
// ordering is deterministic across runs even though the executor fans
// the layer out into goroutines.
func (f *Flow) TopoLayers() ([][]string, error) {
	indeg := make(map[string]int, len(f.Nodes))
	adj := make(map[string][]string, len(f.Nodes))
	for _, n := range f.Nodes {
		indeg[n.ID] = 0
	}
	for _, e := range f.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		indeg[e.To]++
	}
	// Initial layer: every node with indegree 0, in declaration order.
	var layers [][]string
	current := make([]string, 0)
	for _, n := range f.Nodes {
		if indeg[n.ID] == 0 {
			current = append(current, n.ID)
		}
	}
	visited := 0
	for len(current) > 0 {
		layers = append(layers, current)
		visited += len(current)
		next := map[string]bool{}
		nextOrder := make([]string, 0)
		for _, id := range current {
			for _, dst := range adj[id] {
				indeg[dst]--
				if indeg[dst] == 0 && !next[dst] {
					next[dst] = true
					nextOrder = append(nextOrder, dst)
				}
			}
		}
		current = nextOrder
	}
	if visited != len(f.Nodes) {
		return nil, fmt.Errorf("flow contains a cycle")
	}
	return layers, nil
}

// validateNodeConfig enforces the minimum config keys per node type.
// The executor-time config is richer than the structural validation
// here; this function only catches "obviously incomplete" definitions.
func validateNodeConfig(n Node) error {
	switch n.Type {
	case NodeTypeLLM:
		if cfgString(n.Config, "provider") == "" {
			return errors.New("llm node requires config.provider")
		}
	case NodeTypeTool:
		if cfgString(n.Config, "tool") == "" {
			return errors.New("tool node requires config.tool")
		}
	case NodeTypeIf:
		if cfgString(n.Config, "condition") == "" {
			return errors.New("if node requires config.condition")
		}
	case NodeTypeIterate:
		if cfgString(n.Config, "forEach") == "" {
			return errors.New("iterate node requires config.forEach")
		}
		if _, ok := n.Config["body"].(map[string]any); !ok {
			return errors.New("iterate node requires config.body (a node spec)")
		}
		body, _ := iterateBody(n)
		if body == nil {
			return errors.New("iterate node config.body must be a node spec")
		}
		if !IsKnownNodeType(body.Type) || body.Type == NodeTypeIterate {
			return fmt.Errorf("iterate node body has unsupported type %q", body.Type)
		}
		if err := validateNodeConfig(*body); err != nil {
			return fmt.Errorf("iterate body: %w", err)
		}
	case NodeTypeOutput:
		// keys is optional — an empty output node returns the full state.
	}
	if err := validateRetryConfig(n.Config); err != nil {
		return err
	}
	return nil
}

// validateRetryConfig enforces the optional retry knob shape:
//
//	{"retry": {
//	    "maxAttempts": <0..MaxRetryAttempts>,
//	    "backoffMs":   <int>,
//	    "strategy":    "fixed"|"exponential"  // US-478
//	}}
//
// All inner fields are optional; their absence falls back to flow-level
// defaults at execute time. strategy defaults to "fixed" so pre-US-478
// flows preserve their existing backoff semantics byte-for-byte.
func validateRetryConfig(cfg map[string]any) error {
	raw, ok := cfg["retry"]
	if !ok {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return errors.New("config.retry must be an object")
	}
	if v, has := m["maxAttempts"]; has {
		n, ok := toInt(v)
		if !ok {
			return errors.New("config.retry.maxAttempts must be an integer")
		}
		if n < 0 || n > MaxRetryAttempts {
			return fmt.Errorf("config.retry.maxAttempts must be in [0, %d]", MaxRetryAttempts)
		}
	}
	if v, has := m["backoffMs"]; has {
		n, ok := toInt(v)
		if !ok {
			return errors.New("config.retry.backoffMs must be an integer")
		}
		if n < 0 {
			return errors.New("config.retry.backoffMs must be non-negative")
		}
	}
	if v, has := m["strategy"]; has {
		s, ok := v.(string)
		if !ok {
			return errors.New("config.retry.strategy must be a string")
		}
		switch s {
		case "", BackoffStrategyFixed, BackoffStrategyExponential:
		default:
			return fmt.Errorf("config.retry.strategy %q must be %q or %q",
				s, BackoffStrategyFixed, BackoffStrategyExponential)
		}
	}
	return nil
}

// Backoff strategy constants. US-478 introduces "exponential" alongside
// the existing fixed strategy. Empty / absent strategy defaults to fixed.
const (
	BackoffStrategyFixed       = "fixed"
	BackoffStrategyExponential = "exponential"
)

// iterateBody parses the body field of an iterate node back into a Node.
// Returns (nil, false) when the body is missing or malformed.
func iterateBody(n Node) (*Node, bool) {
	raw, ok := n.Config["body"].(map[string]any)
	if !ok {
		return nil, false
	}
	body := Node{}
	if id, _ := raw["id"].(string); id != "" {
		body.ID = id
	} else {
		body.ID = n.ID + ".body"
	}
	body.Type, _ = raw["type"].(string)
	if cfg, ok := raw["config"].(map[string]any); ok {
		body.Config = cfg
	}
	return &body, true
}

// toInt coerces a JSON-decoded number to an int. JSON numbers come back
// as float64 by default; tests / fixtures sometimes pass int literals
// directly so accept both shapes.
func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int32:
		return int(x), true
	case int64:
		return int(x), true
	case float32:
		return int(x), x == float32(int(x))
	case float64:
		return int(x), x == float64(int(x))
	}
	return 0, false
}

// cfgString reads a string from a node config map. Returns "" when
// missing or non-string.
func cfgString(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
