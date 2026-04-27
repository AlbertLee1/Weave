package aip

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
)

// ErrToolNotFound is returned by ToolRegistry.Get when the named tool
// has not been registered. The SendMessage handler maps this to a
// structured AIPToolNotFound 500 so operators see the misconfiguration
// (vs the LLM hallucinating a tool name into the conversation).
var ErrToolNotFound = errors.New("aip: tool not found")

// ToolHandler is the server-side implementation of one declared tool.
// Definition() returns the wire shape the handler hands to the model
// via ChatRequest.Tools; Execute receives the model-produced argument
// blob and returns a string result that becomes the RoleTool message
// content the model sees on the next turn.
//
// Implementations must be safe for concurrent use — a single ToolHandler
// instance is shared across every SendMessage that invokes it.
type ToolHandler interface {
	Definition() ToolDef
	Execute(ctx context.Context, args json.RawMessage) (string, error)
}

// ToolRegistry is the lookup hub for ToolHandler. The SendMessage
// handler resolves model-requested tools through this registry; an
// unset / empty registry means tools are disabled and the loop runs
// at most one Provider.Complete call.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]ToolHandler
}

// NewToolRegistry constructs an empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: map[string]ToolHandler{}}
}

// Register stores h under h.Definition().Name. Empty / nil are no-ops;
// re-registering the same name overwrites the prior entry.
func (r *ToolRegistry) Register(h ToolHandler) {
	if r == nil || h == nil {
		return
	}
	name := strings.TrimSpace(h.Definition().Name)
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = h
}

// Get returns the named handler or ErrToolNotFound.
func (r *ToolRegistry) Get(name string) (ToolHandler, error) {
	if r == nil {
		return nil, ErrToolNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	h, ok := r.tools[name]
	if !ok {
		return nil, ErrToolNotFound
	}
	return h, nil
}

// Definitions returns every registered ToolDef in stable name order.
// Used by SendMessage to build ChatRequest.Tools from the registry.
func (r *ToolRegistry) Definitions() []ToolDef {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.tools) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.tools))
	for k := range r.tools {
		names = append(names, k)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	out := make([]ToolDef, 0, len(names))
	for _, n := range names {
		out = append(out, r.tools[n].Definition())
	}
	return out
}

// Names returns every registered tool name in stable order.
func (r *ToolRegistry) Names() []string {
	defs := r.Definitions()
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.Name)
	}
	return out
}

// EchoToolHandler is a deterministic ToolHandler used in tests and as
// a "hello world" template. It echoes the args.text field back as the
// tool result string. Production deployments register custom handlers
// for the LLM to call (database queries, http fetches, etc.).
type EchoToolHandler struct{}

// Definition returns the standard echo tool descriptor.
func (e *EchoToolHandler) Definition() ToolDef {
	return ToolDef{
		Name:        "echo",
		Description: "Echo the input text unchanged",
		Parameters: json.RawMessage(`{
            "type": "object",
            "properties": {
                "text": {"type": "string", "description": "Text to echo back"}
            },
            "required": ["text"]
        }`),
	}
}

// Execute parses args.text and returns it as the tool result.
func (e *EchoToolHandler) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Text string `json:"text"`
	}
	if len(args) > 0 {
		if err := json.Unmarshal(args, &p); err != nil {
			return "", err
		}
	}
	return p.Text, nil
}
