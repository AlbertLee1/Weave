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

// openAIDefaultBaseURL is the production OpenAI chat-completions root.
// Tests override via OpenAIConfig.BaseURL pointing at httptest.NewServer.
const openAIDefaultBaseURL = "https://api.openai.com/v1"

// openAIDefaultModel is the latest small chat-completions model. Picked
// to mirror pkg/ai.OpenAIProvider so every "OpenAI-backed" surface in
// Weave defaults to the same SKU.
const openAIDefaultModel = "gpt-4o-mini"

// OpenAIConfig parameterises OpenAIProvider. Only APIKey is required
// in production; the other fields fall back to defaults.
type OpenAIConfig struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

// OpenAIProvider implements Provider against the OpenAI chat-completions
// HTTP API. Safe for concurrent use; per-request state lives in Complete.
type OpenAIProvider struct {
	cfg    OpenAIConfig
	client *http.Client
}

// NewOpenAIProvider constructs an OpenAIProvider from cfg. Defaults
// are filled in for any zero-valued fields.
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = openAIDefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = openAIDefaultModel
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &OpenAIProvider{cfg: cfg, client: client}
}

// Name returns ProviderOpenAI.
func (p *OpenAIProvider) Name() string { return ProviderOpenAI }

type openAIChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatRequest struct {
	Model       string              `json:"model"`
	Messages    []openAIChatMessage `json:"messages"`
	Temperature float64             `json:"temperature,omitempty"`
	MaxTokens   int                 `json:"max_tokens,omitempty"`
}

type openAIChatResponseChoice struct {
	Message openAIChatMessage `json:"message"`
}

type openAIChatUsage struct {
	TotalTokens int `json:"total_tokens"`
}

type openAIChatResponse struct {
	Model   string                     `json:"model"`
	Choices []openAIChatResponseChoice `json:"choices"`
	Usage   openAIChatUsage            `json:"usage"`
}

type openAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Complete POSTs req to /chat/completions and returns the assistant
// response. Returns ErrProviderNotConfigured wrapped when APIKey is
// empty so the handler can map it to a structured 503.
func (p *OpenAIProvider) Complete(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if p.cfg.APIKey == "" {
		return nil, fmt.Errorf("%w: openai api key missing", ErrProviderNotConfigured)
	}
	model := req.Model
	if model == "" {
		model = p.cfg.Model
	}

	msgs := make([]openAIChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		msgs = append(msgs, openAIChatMessage{Role: m.Role, Content: m.Content})
	}
	body := openAIChatRequest{
		Model:       model,
		Messages:    msgs,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("openai chat: marshal request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("openai chat: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai chat: http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai chat: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody openAIErrorBody
		if jerr := json.Unmarshal(respBody, &errBody); jerr == nil && errBody.Error.Message != "" {
			return nil, fmt.Errorf("openai chat: %d %s: %s",
				resp.StatusCode, errBody.Error.Type, errBody.Error.Message)
		}
		return nil, fmt.Errorf("openai chat: %d: %s",
			resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed openAIChatResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("openai chat: parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openai chat: empty choices")
	}
	out := &ChatResponse{
		Content:    parsed.Choices[0].Message.Content,
		Model:      parsed.Model,
		TokenCount: parsed.Usage.TotalTokens,
	}
	if out.Model == "" {
		out.Model = model
	}
	return out, nil
}
