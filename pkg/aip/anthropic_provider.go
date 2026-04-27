package aip

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// anthropicDefaultBaseURL is the production Anthropic API root. Tests
// override via AnthropicConfig.BaseURL pointing at httptest.NewServer.
const anthropicDefaultBaseURL = "https://api.anthropic.com/v1"

// anthropicDefaultModel mirrors the most recent Sonnet release alias
// used elsewhere in this codebase. Per Anthropic docs the messages API
// expects an explicit model id on every request.
const anthropicDefaultModel = "claude-sonnet-4-6"

// anthropicAPIVersion is the value of the anthropic-version header.
// Pinned so a future API revision cannot silently change response shape.
const anthropicAPIVersion = "2023-06-01"

// anthropicDefaultMaxTokens is the floor we apply when the caller does
// not specify MaxTokens — Anthropic's messages API requires the field.
const anthropicDefaultMaxTokens = 1024

// AnthropicConfig parameterises AnthropicProvider. APIKey is required;
// other fields fall back to defaults.
type AnthropicConfig struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

// AnthropicProvider implements Provider against the Anthropic Messages
// HTTP API. Safe for concurrent use.
type AnthropicProvider struct {
	cfg    AnthropicConfig
	client *http.Client
}

// NewAnthropicProvider constructs an AnthropicProvider from cfg.
func NewAnthropicProvider(cfg AnthropicConfig) *AnthropicProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = anthropicDefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = anthropicDefaultModel
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &AnthropicProvider{cfg: cfg, client: client}
}

// Name returns ProviderAnthropic.
func (p *AnthropicProvider) Name() string { return ProviderAnthropic }

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicChatRequest struct {
	Model       string             `json:"model"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature float64            `json:"temperature,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicChatResponse struct {
	Model   string                  `json:"model"`
	Content []anthropicContentBlock `json:"content"`
	Usage   anthropicUsage          `json:"usage"`
}

type anthropicErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete POSTs req to /messages and returns the assistant response.
// Anthropic's messages API splits system from user/assistant messages,
// so we walk req.Messages and route the first system entry into the
// top-level "system" field; remaining system entries are dropped.
func (p *AnthropicProvider) Complete(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if p.cfg.APIKey == "" {
		return nil, fmt.Errorf("%w: anthropic api key missing", ErrProviderNotConfigured)
	}
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = anthropicDefaultMaxTokens
	}

	var system string
	msgs := make([]anthropicMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			if system == "" {
				system = m.Content
			}
			continue
		}
		msgs = append(msgs, anthropicMessage{Role: m.Role, Content: m.Content})
	}

	body := anthropicChatRequest{
		Model:       model,
		System:      system,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   maxTokens,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("anthropic messages: marshal request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/messages"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("anthropic messages: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.cfg.APIKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic messages: http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("anthropic messages: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody anthropicErrorBody
		if jerr := json.Unmarshal(respBody, &errBody); jerr == nil && errBody.Error.Message != "" {
			return nil, fmt.Errorf("anthropic messages: %d %s: %s",
				resp.StatusCode, errBody.Error.Type, errBody.Error.Message)
		}
		return nil, fmt.Errorf("anthropic messages: %d: %s",
			resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed anthropicChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic messages: parse response: %w", err)
	}

	var content strings.Builder
	for _, block := range parsed.Content {
		if block.Type == "text" {
			content.WriteString(block.Text)
		}
	}
	if content.Len() == 0 {
		return nil, fmt.Errorf("anthropic messages: empty content")
	}
	out := &ChatResponse{
		Content:    content.String(),
		Model:      parsed.Model,
		TokenCount: parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
	}
	if out.Model == "" {
		out.Model = model
	}
	return out, nil
}
