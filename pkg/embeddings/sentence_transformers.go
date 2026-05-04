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

// stDefaultModel is the sentence-transformers community baseline (384 dims).
const stDefaultModel = "sentence-transformers/all-MiniLM-L6-v2"

// stDefaultDimensions matches all-MiniLM-L6-v2. Override via
// SentenceTransformersConfig.Dimensions for other models (e.g.
// all-mpnet-base-v2 = 768, all-roberta-large-v1 = 1024).
const stDefaultDimensions = 384

// SentenceTransformersConfig parameterises a HTTP-shim sentence-transformers
// provider. The shim is expected to expose:
//
//	POST {BaseURL}/embed
//	{ "texts": ["...", "..."], "model": "..." }
//	→ { "embeddings": [[...], [...]] }
//
// This shape keeps the Go side language-agnostic: any Python, ONNX, or
// remote inference backend can satisfy it. BaseURL is REQUIRED — there is
// no sensible production default for an arbitrary inference shim.
type SentenceTransformersConfig struct {
	// BaseURL is the inference shim root, e.g. "http://localhost:8000".
	// Trailing slashes are tolerated. Empty BaseURL → Embed returns a
	// configuration error.
	BaseURL string

	// Model is the model identifier passed to the shim. Defaults to
	// sentence-transformers/all-MiniLM-L6-v2.
	Model string

	// Dimensions is the vector dimensionality the chosen model emits. The
	// caller MUST configure this to match the model's actual output (the
	// provider validates against it on every Embed).
	Dimensions int

	// APIKey, when non-empty, is sent as `Authorization: Bearer <key>`.
	APIKey string

	// HTTPClient overrides the default *http.Client. nil → 60s-timeout
	// default (sentence-transformers cold-start can exceed 30s).
	HTTPClient *http.Client
}

// SentenceTransformersProvider implements Provider against a HTTP shim
// hosting sentence-transformers (or any embedding model exposed via the
// same wire shape). Safe for concurrent use.
type SentenceTransformersProvider struct {
	cfg    SentenceTransformersConfig
	client *http.Client
}

// NewSentenceTransformersProvider constructs a provider with defaults
// applied for any zero-valued fields except BaseURL, which has no
// reasonable default.
func NewSentenceTransformersProvider(cfg SentenceTransformersConfig) *SentenceTransformersProvider {
	if cfg.Model == "" {
		cfg.Model = stDefaultModel
	}
	if cfg.Dimensions <= 0 {
		cfg.Dimensions = stDefaultDimensions
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &SentenceTransformersProvider{cfg: cfg, client: client}
}

// Model returns the configured model identifier.
func (p *SentenceTransformersProvider) Model() string { return p.cfg.Model }

// Dimensions returns the configured vector dimensionality.
func (p *SentenceTransformersProvider) Dimensions() int { return p.cfg.Dimensions }

type stEmbedRequest struct {
	Texts []string `json:"texts"`
	Model string   `json:"model,omitempty"`
}

type stEmbedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Model      string      `json:"model,omitempty"`
}

// Embed POSTs the texts to {BaseURL}/embed and returns the vectors in
// input order. Validates that the response carries len(texts) vectors,
// each of Dimensions() floats.
func (p *SentenceTransformersProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if p.cfg.BaseURL == "" {
		return nil, fmt.Errorf("sentence-transformers embeddings: missing BaseURL (set WEAVE_EMBED_ST_BASE_URL)")
	}
	if len(texts) == 0 {
		return [][]float32{}, nil
	}

	body, err := json.Marshal(stEmbedRequest{Texts: texts, Model: p.cfg.Model})
	if err != nil {
		return nil, fmt.Errorf("sentence-transformers embeddings: marshal request: %w", err)
	}

	url := strings.TrimRight(p.cfg.BaseURL, "/") + "/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("sentence-transformers embeddings: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sentence-transformers embeddings: http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("sentence-transformers embeddings: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("sentence-transformers embeddings: %d: %s",
			resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var parsed stEmbedResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("sentence-transformers embeddings: parse response: %w", err)
	}
	if len(parsed.Embeddings) != len(texts) {
		return nil, fmt.Errorf("sentence-transformers embeddings: expected %d vectors, got %d",
			len(texts), len(parsed.Embeddings))
	}
	for i, v := range parsed.Embeddings {
		if len(v) != p.cfg.Dimensions {
			return nil, fmt.Errorf("sentence-transformers embeddings: vector %d has %d dims, expected %d",
				i, len(v), p.cfg.Dimensions)
		}
	}
	return parsed.Embeddings, nil
}
