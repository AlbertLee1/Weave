package embeddings

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

// ollamaDefaultBaseURL is the canonical Ollama daemon root.
// Tests override via OllamaConfig.BaseURL.
const ollamaDefaultBaseURL = "http://localhost:11434"

// ollamaDefaultModel is Ollama's standard 768-dim text embedding model.
const ollamaDefaultModel = "nomic-embed-text"

// ollamaDefaultDimensions matches nomic-embed-text. Override via
// OllamaConfig.Dimensions when using e.g. mxbai-embed-large (1024) or a
// MiniLM-style 384-dim model loaded into Ollama.
const ollamaDefaultDimensions = 768

// OllamaConfig parameterises the OllamaProvider. All fields are optional —
// an empty OllamaConfig{} talks to a local Ollama daemon using the
// nomic-embed-text model at 768 dimensions.
type OllamaConfig struct {
	// BaseURL is the Ollama daemon root, e.g. "http://localhost:11434".
	// Trailing slashes are tolerated.
	BaseURL string

	// Model is the embedding model identifier, e.g. "nomic-embed-text" or
	// "mxbai-embed-large". Defaults to nomic-embed-text.
	Model string

	// Dimensions is the vector dimensionality the chosen model emits.
	// Different Ollama models output different dims (384 / 768 / 1024 /
	// 1536) so callers MUST set this to match their model when overriding.
	Dimensions int

	// HTTPClient overrides the default *http.Client. nil → 30s-timeout
	// default.
	HTTPClient *http.Client
}

// OllamaProvider implements Provider against the Ollama daemon's batch
// embeddings endpoint (POST /api/embed). Safe for concurrent use.
type OllamaProvider struct {
	cfg    OllamaConfig
	client *http.Client
}

// NewOllamaProvider constructs an OllamaProvider with sensible defaults
// applied for any zero-valued fields.
func NewOllamaProvider(cfg OllamaConfig) *OllamaProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = ollamaDefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = ollamaDefaultModel
	}
	if cfg.Dimensions <= 0 {
		cfg.Dimensions = ollamaDefaultDimensions
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OllamaProvider{cfg: cfg, client: client}
}

// Model returns the configured Ollama model identifier.
func (p *OllamaProvider) Model() string { return p.cfg.Model }

// Dimensions returns the configured vector dimensionality.
func (p *OllamaProvider) Dimensions() int { return p.cfg.Dimensions }

type ollamaEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type ollamaEmbedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed POSTs the texts to /api/embed and returns vectors in input order.
// Ollama's response shape preserves the input order so no re-indexing is
// required (unlike OpenAI's index-keyed entries).
func (p *OllamaProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	body, err := json.Marshal(ollamaEmbedRequest{Model: p.cfg.Model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("ollama embeddings: marshal request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/api/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ollama embeddings: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embeddings: http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama embeddings: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("ollama embeddings: %d: %s",
			resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed ollamaEmbedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("ollama embeddings: parse response: %w", err)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama embeddings: expected %d vectors, got %d",
			len(texts), len(parsed.Embeddings))
	}
	for i, v := range parsed.Embeddings {
		if len(v) != p.cfg.Dimensions {
			return nil, fmt.Errorf("ollama embeddings: vector %d has %d dims, expected %d",
				i, len(v), p.cfg.Dimensions)
		}
	}
	return parsed.Embeddings, nil
}
