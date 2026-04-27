package aip

import (
	"context"
	"fmt"
	"strings"
)

// MockProvider is the deterministic in-process Provider used in tests
// and as the default for local-dev / CI deployments without external
// API keys. It echoes the latest user message with a deterministic
// prefix so callers can assert on the response shape without mocking
// HTTP transport.
type MockProvider struct {
	// Prefix is prepended to the echoed content. Defaults to
	// "[mock] " when empty so the response is visibly distinguishable
	// from a real LLM reply.
	Prefix string
}

// NewMockProvider returns a MockProvider with the default prefix.
func NewMockProvider() *MockProvider { return &MockProvider{} }

// Name returns ProviderMock.
func (p *MockProvider) Name() string { return ProviderMock }

// Complete returns a deterministic echo of the last user message in
// req.Messages. Empty conversations get a generic greeting so the
// response is never empty.
func (p *MockProvider) Complete(_ context.Context, req ChatRequest) (*ChatResponse, error) {
	prefix := p.Prefix
	if prefix == "" {
		prefix = "[mock] "
	}
	last := lastUserContent(req.Messages)
	model := req.Model
	if model == "" {
		model = "weave-mock-llm-v1"
	}
	if last == "" {
		return &ChatResponse{
			Content: prefix + "hello",
			Model:   model,
		}, nil
	}
	reply := fmt.Sprintf("%secho: %s", prefix, strings.TrimSpace(last))
	return &ChatResponse{
		Content:    reply,
		Model:      model,
		TokenCount: len(reply) / 4,
	}, nil
}

// lastUserContent walks msgs from the end and returns the content of
// the most recent RoleUser entry. Returns "" when no user message is
// present.
func lastUserContent(msgs []ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}
