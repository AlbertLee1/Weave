package aip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// ChatRequest is the input to Provider.Complete. The slice carries the
// full conversation context the LLM should see, including any system
// anchor messages and prior assistant responses. Implementations must
// not mutate the slice or its elements.
type ChatRequest struct {
	Model       string
	Messages    []ChatMessage
	Temperature float64
	MaxTokens   int
}

// ChatMessage is one role/content pair the provider receives. The Role
// values match RoleSystem / RoleUser / RoleAssistant from thread.go.
type ChatMessage struct {
	Role    string
	Content string
}

// ChatResponse is the result returned by Provider.Complete. Content is
// the assistant's reply; Model echoes back the model that produced it
// so callers can persist it on the assistant Message row alongside an
// optional token count for ledger / quota purposes.
type ChatResponse struct {
	Content    string
	Model      string
	TokenCount int
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
