package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrToolAlreadyRegistered is returned by Registry.Register when a tool with
// the same name has already been registered.
var ErrToolAlreadyRegistered = errors.New("tool already registered")

// ErrToolNotFound is returned by Registry.Call when the requested tool name
// has not been registered. Callers translate this to JSON-RPC -32601.
var ErrToolNotFound = errors.New("tool not found")

// Registry is a transport-independent collection of MCP tools. It is safe
// for concurrent use; List and Call may be invoked from any goroutine.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

// Register adds a tool to the registry. Returns ErrToolAlreadyRegistered if
// a tool with the same name already exists.
func (r *Registry) Register(t Tool) error {
	if t == nil {
		return errors.New("nil tool")
	}
	def := t.Definition()
	if def.Name == "" {
		return errors.New("tool definition has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tools[def.Name]; ok {
		return fmt.Errorf("%w: %s", ErrToolAlreadyRegistered, def.Name)
	}
	r.tools[def.Name] = t
	return nil
}

// List returns the registered tool definitions sorted by name. The sort is
// deterministic so the wire response is stable across calls — important for
// AI clients that cache tool catalogues.
func (r *Registry) List() []ToolDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		out = append(out, t.Definition())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get returns the registered tool by name, or false if no such tool exists.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Call dispatches an args map to a registered tool. The arguments are
// validated against the tool's declared input schema before invocation; a
// validation failure returns a wrapped error and the tool is never called.
func (r *Registry) Call(ctx context.Context, name string, args map[string]any) (*ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrToolNotFound, name)
	}
	def := tool.Definition()
	if err := def.ValidateArgs(args); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	return tool.Call(ctx, args)
}
