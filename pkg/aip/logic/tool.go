package logic

import (
	"context"
	"strings"
	"sync"
)

// MapToolRegistry is the simplest non-trivial ToolRegistry — a string
// map of registered Tool instances. Safe for concurrent reads;
// Register is intended to be called at boot only.
type MapToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewMapToolRegistry returns a MapToolRegistry seeded with the built-in
// tools (echo / concat). Operators add custom tools via Register.
func NewMapToolRegistry() *MapToolRegistry {
	r := &MapToolRegistry{tools: map[string]Tool{}}
	r.Register(&EchoTool{})
	r.Register(&ConcatTool{})
	return r
}

// Register adds t under t.Name(). Calls with empty / nil are no-ops.
func (r *MapToolRegistry) Register(t Tool) {
	if r == nil || t == nil {
		return
	}
	name := strings.TrimSpace(t.Name())
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = t
}

// Lookup returns the named tool. The returned bool reports whether a
// match was found.
func (r *MapToolRegistry) Lookup(name string) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// Names returns every registered tool name in stable order. Useful for
// admin / diagnostics endpoints.
func (r *MapToolRegistry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.tools))
	for k := range r.tools {
		out = append(out, k)
	}
	// Tiny insertion sort — keeps us off the sort import.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// EchoTool returns its `text` parameter unchanged. The simplest tool
// that exercises the params-substitution pipeline; useful for tests
// and as a "hello world" template for custom tools.
type EchoTool struct{}

// Name returns "echo".
func (e *EchoTool) Name() string { return "echo" }

// Invoke returns {text: <params.text>}.
func (e *EchoTool) Invoke(_ context.Context, params map[string]any) (map[string]any, error) {
	text, _ := params["text"].(string)
	return map[string]any{"text": text}, nil
}

// ConcatTool joins every params[*] string value. Used by tests as a
// trivial multi-input tool. Non-string params are ignored.
type ConcatTool struct{}

// Name returns "concat".
func (c *ConcatTool) Name() string { return "concat" }

// Invoke joins every string-valued param key (in sorted-key order to
// stay deterministic) with the configured separator (default ":").
func (c *ConcatTool) Invoke(_ context.Context, params map[string]any) (map[string]any, error) {
	sep, _ := params["separator"].(string)
	if sep == "" {
		sep = ":"
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		if k == "separator" {
			continue
		}
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		s, ok := params[k].(string)
		if !ok {
			continue
		}
		parts = append(parts, s)
	}
	return map[string]any{"text": strings.Join(parts, sep)}, nil
}
