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

// openAIDefaultBaseURL is the canonical OpenAI API endpoint root.
// Tests override this with httptest.NewServer.URL via OpenAIConfig.BaseURL.
const openAIDefaultBaseURL = "https://api.openai.com/v1"

// openAIDefaultModel mirrors the schema column dimensionality (1536).
// We deliberately do NOT default to ada-002 — Anthropic / OpenAI both
// recommend text-embedding-3-small as the modern entry-level model and
// it shares the same 1536 dimension count.
const openAIDefaultModel = "text-embedding-3-small"

// openAIDimensions is the fixed output size for ada-002 and 3-small. The
// schema column is `vector(1536)`, so this MUST stay 1536 unless the
// migration is updated to use a different vector dimension.
const openAIDimensions = 1536

// OpenAIConfig parameterises the OpenAIProvider. Only APIKey is required;
// the other fields fall back to sensible production defaults so callers
// can pass `OpenAIConfig{APIKey: os.Getenv("OPENAI_API_KEY")}` and have it
// just work in production.
type OpenAIConfig struct {
	// APIKey is the OPENAI_API_KEY value used as the bearer token.
	APIKey string

	// BaseURL is the API root, e.g. "https://api.openai.com/v1". Tests
	// override this to point at httptest.NewServer.URL. Trailing slashes
	// are tolerated.
	BaseURL string

	// Model is the embedding model identifier. Defaults to
	// text-embedding-3-small.
	Model string

	// HTTPClient overrides the default *http.Client. Useful for tests
	// that need to capture transport-level state. nil → default with a
	// 30s timeout.
	HTTPClient *http.Client
}

// OpenAIProvider implements Provider against the OpenAI HTTP embeddings
// API. It is safe for concurrent use because all per-request state lives
// inside the Embed call's local scope.
type OpenAIProvider struct {
	cfg    OpenAIConfig
	client *http.Client
}

// NewOpenAIProvider constructs an OpenAIProvider from cfg. Defaults are
// applied for any zero-valued fields.
func NewOpenAIProvider(cfg OpenAIConfig) *OpenAIProvider {
	if cfg.BaseURL == "" {
		cfg.BaseURL = openAIDefaultBaseURL
	}
	if cfg.Model == "" {
		cfg.Model = openAIDefaultModel
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OpenAIProvider{cfg: cfg, client: client}
}

// Model returns the configured embedding model identifier.
func (p *OpenAIProvider) Model() string { return p.cfg.Model }

// Dimensions returns the OpenAI vector dimensionality (always 1536 for
// ada-002 / text-embedding-3-small).
func (p *OpenAIProvider) Dimensions() int { return openAIDimensions }

// openAIEmbeddingRequest is the JSON shape POSTed to /v1/embeddings.
type openAIEmbeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// openAIEmbeddingDatum is one entry in the OpenAI response `data` array.
type openAIEmbeddingDatum struct {
	Index     int       `json:"index"`
	Embedding []float32 `json:"embedding"`
}

// openAIEmbeddingResponse is the top-level body returned by /v1/embeddings.
type openAIEmbeddingResponse struct {
	Data []openAIEmbeddingDatum `json:"data"`
}

// openAIErrorBody represents the error wrapper OpenAI returns on non-2xx.
type openAIErrorBody struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Embed POSTs the texts to the OpenAI embeddings endpoint and returns
// the vectors in input order. Errors include the upstream message when
// available so operators can debug auth / quota issues quickly.
func (p *OpenAIProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if p.cfg.APIKey == "" {
		return nil, fmt.Errorf("openai embeddings: missing API key (set OPENAI_API_KEY)")
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	body, err := json.Marshal(openAIEmbeddingRequest{
		Model: p.cfg.Model,
		Input: texts,
	})
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: marshal request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/embeddings"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("openai embeddings: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var errBody openAIErrorBody
		if jerr := json.Unmarshal(respBody, &errBody); jerr == nil && errBody.Error.Message != "" {
			return nil, fmt.Errorf("openai embeddings: %d %s: %s",
				resp.StatusCode, errBody.Error.Type, errBody.Error.Message)
		}
		return nil, fmt.Errorf("openai embeddings: %d: %s",
			resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed openAIEmbeddingResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("openai embeddings: parse response: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("openai embeddings: expected %d vectors, got %d",
			len(texts), len(parsed.Data))
	}

	// OpenAI returns the data array with index fields — we sort by index
	// to be safe even though the API documents the order as preserved.
	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(out) {
			return nil, fmt.Errorf("openai embeddings: invalid index %d", d.Index)
		}
		out[d.Index] = d.Embedding
	}
	return out, nil
}
