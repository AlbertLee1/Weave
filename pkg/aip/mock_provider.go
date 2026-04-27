package aip

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MockProvider is the deterministic in-process Provider used in tests
// and as the default for local-dev / CI deployments without external
// API keys. It echoes the latest user message with a deterministic
// prefix so callers can assert on the response shape without mocking
// HTTP transport.
//
// Tool-calling behaviour (US-284): when ChatRequest.Tools is non-empty
// AND the conversation does NOT yet contain a RoleTool result for
// every declared tool, the mock returns a deterministic ToolCall
// targeting the first declared tool with the user's last message
// passed as the {"text": ...} argument. Once a tool result is observed
// in the history, the next Complete returns a final assistant text
// reply that incorporates the tool result. This shape exercises the
// full function-calling loop in tests without needing a live LLM.
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
	model := req.Model
	if model == "" {
		model = "weave-mock-llm-v1"
	}

	if len(req.Tools) > 0 && !hasToolResult(req.Messages) {
		args, _ := json.Marshal(map[string]string{
			"text": strings.TrimSpace(lastUserContent(req.Messages)),
		})
		return &ChatResponse{
			Model: model,
			ToolCalls: []ToolCall{{
				ID:        fmt.Sprintf("call_mock_%s", req.Tools[0].Name),
				Name:      req.Tools[0].Name,
				Arguments: args,
			}},
		}, nil
	}

	last := lastUserContent(req.Messages)
	if len(req.Tools) > 0 {
		toolText := lastToolResult(req.Messages)
		reply := fmt.Sprintf("%stool-result: %s", prefix, toolText)
		return &ChatResponse{
			Content:    reply,
			Model:      model,
			TokenCount: len(reply) / 4,
		}, nil
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

// hasToolResult reports whether msgs contains any RoleTool entry.
func hasToolResult(msgs []ChatMessage) bool {
	for _, m := range msgs {
		if m.Role == RoleTool {
			return true
		}
	}
	return false
}

// lastToolResult returns the content of the most recent RoleTool
// message, or "" when none is present.
func lastToolResult(msgs []ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == RoleTool {
			return msgs[i].Content
		}
	}
	return ""
}
