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
	NodeTypeLLM    = "llm"
	NodeTypeTool   = "tool"
	NodeTypeIf     = "if"
	NodeTypeOutput = "output"
)

// IsKnownNodeType reports whether name is a built-in node type.
func IsKnownNodeType(name string) bool {
	switch name {
	case NodeTypeLLM, NodeTypeTool, NodeTypeIf, NodeTypeOutput:
		return true
	}
	return false
}

// KnownNodeTypes returns the canonical list in stable order.
func KnownNodeTypes() []string {
	return []string{NodeTypeLLM, NodeTypeTool, NodeTypeIf, NodeTypeOutput}
}

// Flow is one persisted Logic Flow row. Nodes carry the executable
// configuration; Edges describe data flow between nodes.
type Flow struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Nodes       []Node    `json:"nodes"`
	Edges       []Edge    `json:"edges"`
	CreatedBy   string    `json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// FlowUpdate is the partial-update payload. Pointer fields preserve
// "omit=keep current" semantics.
type FlowUpdate struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Nodes       *[]Node `json:"nodes,omitempty"`
	Edges       *[]Edge `json:"edges,omitempty"`
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
// "success", "skipped", or "failed".
type TraceEntry struct {
	NodeID string         `json:"nodeId"`
	Type   string         `json:"type"`
	Status string         `json:"status"`
	Output map[string]any `json:"output,omitempty"`
	Error  string         `json:"error,omitempty"`
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
	if _, err := f.TopoOrder(); err != nil {
		return err
	}
	return nil
}

// TopoOrder returns node IDs in a topological order such that every
// edge goes from an earlier ID to a later ID. Returns an error if the
// graph contains a cycle.
func (f *Flow) TopoOrder() ([]string, error) {
	indeg := make(map[string]int, len(f.Nodes))
	adj := make(map[string][]string, len(f.Nodes))
	for _, n := range f.Nodes {
		indeg[n.ID] = 0
	}
	for _, e := range f.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		indeg[e.To]++
	}
	// Use the original node-declaration order as a tie-breaker so the
	// output is deterministic across runs.
	queue := make([]string, 0)
	for _, n := range f.Nodes {
		if indeg[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	out := make([]string, 0, len(f.Nodes))
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		out = append(out, head)
		for _, dst := range adj[head] {
			indeg[dst]--
			if indeg[dst] == 0 {
				queue = append(queue, dst)
			}
		}
	}
	if len(out) != len(f.Nodes) {
		return nil, fmt.Errorf("flow contains a cycle")
	}
	return out, nil
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
	case NodeTypeOutput:
		// keys is optional — an empty output node returns the full state.
	}
	return nil
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
