package aip

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ChatRequest is the input to Provider.Complete. The slice carries the
// full conversation context the LLM should see, including any system
// anchor messages and prior assistant responses. Implementations must
// not mutate the slice or its elements.
//
// Tools (US-284) declares the function-calling toolbox the model is
// allowed to invoke; an empty slice means the request is plain chat
// without function-calling. Providers that don't support tool calling
// MUST ignore the field and return ToolCalls=nil.
type ChatRequest struct {
	Model       string
	Messages    []ChatMessage
	Temperature float64
	MaxTokens   int
	Tools       []ToolDef
}

// ChatMessage is one role/content pair the provider receives. The Role
// values match RoleSystem / RoleUser / RoleAssistant / RoleTool from
// thread.go. ToolCalls is set on assistant rows that requested function
// invocations; ToolCallID + ToolName are set on RoleTool rows that
// carry a tool's result back to the model.
type ChatMessage struct {
	Role       string
	Content    string
	ToolCalls  []ToolCall
	ToolCallID string
	ToolName   string
}

// ChatResponse is the result returned by Provider.Complete. Content is
// the assistant's reply; Model echoes back the model that produced it
// so callers can persist it on the assistant Message row alongside an
// optional token count for ledger / quota purposes. ToolCalls (US-284)
// is non-empty when the model wants to invoke one or more tools before
// producing its final reply; in that case Content is typically empty
// and the caller is expected to execute the tools, append the results
// as RoleTool messages, and call Complete again.
type ChatResponse struct {
	Content    string
	Model      string
	TokenCount int
	ToolCalls  []ToolCall
}

// ToolDef is a single function/tool the model may invoke during
// Complete. Parameters is a JSON Schema object describing the tool's
// arguments; the provider serialises it verbatim so the format is
// whatever the upstream LLM API accepts (currently a JSON Schema
// fragment is the universal shape across OpenAI / Anthropic / etc.).
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is one tool invocation requested by the model. ID is the
// opaque correlation handle the model uses to match the tool result
// back to the call (mirrors OpenAI's `tool_call_id`); callers MUST
// preserve it on the corresponding RoleTool message. Arguments is the
// raw JSON object the model produced for this call — providers do not
// validate it against ToolDef.Parameters.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// ErrProviderNotConfigured is returned by SendMessage when a thread is
// pinned to a provider that has not been registered (e.g. an OpenAI
// thread on a deployment without OPENAI_API_KEY). The handler maps it
// to a 503 so the operator sees the misconfiguration clearly.
var ErrProviderNotConfigured = errors.New("aip: provider not configured")

// Provider is the abstraction every LLM backend implements. Returns
// ErrProviderNotConfigured (or wraps it via fmt.Errorf("...: %w", err))
// when the provider is wired but missing required credentials.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req ChatRequest) (*ChatResponse, error)
}

// Registry is the multi-provider lookup hub. Threads carry a `provider`
// string; when a SendMessage call lands on a thread the handler dispatches
// through the Registry to the named Provider. Registry is safe for
// concurrent reads; Register is intended to be called at boot only.
type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{providers: map[string]Provider{}}
}

// Register stores p under p.Name(). A second Register with the same name
// overwrites the previous entry — the typical wiring path is to call
// Register once per known provider at boot.
func (r *Registry) Register(p Provider) {
	if r == nil || p == nil {
		return
	}
	name := strings.TrimSpace(p.Name())
	if name == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = p
}

// Get returns the provider registered under name, or nil + a wrapped
// ErrProviderNotConfigured when no provider matches.
func (r *Registry) Get(name string) (Provider, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: %q (registry not wired)", ErrProviderNotConfigured, name)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q (no factory)", ErrProviderNotConfigured, name)
	}
	return p, nil
}

// Names returns every registered provider name in a stable order.
// Useful for diagnostics endpoints / boot-log output.
func (r *Registry) Names() []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.providers))
	for k := range r.providers {
		out = append(out, k)
	}
	// Stable order: mock / openai / anthropic first if present, then
	// any custom providers in insertion-time-but-let's-just-sort order.
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	// Tiny insertion sort to avoid pulling sort into a leaf package.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
