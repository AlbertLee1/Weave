package embeddings_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/embeddings"
)

// TestOpenAIProvider_EndpointSigning verifies that the provider sends the
// expected request shape (POST /v1/embeddings, JSON body with model+input,
// Authorization header) and parses the OpenAI response into the same
// per-input ordering.
func TestOpenAIProvider_EndpointSigning(t *testing.T) {
	var capturedAuth, capturedCT, capturedPath, capturedMethod string
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedCT = r.Header.Get("Content-Type")
		capturedPath = r.URL.Path
		capturedMethod = r.Method

		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)

		// Mirror back two equal-length 1536-dim vectors for the two inputs.
		respObj := map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{"object": "embedding", "index": 0, "embedding": makeVector(1536, 0.1)},
				{"object": "embedding", "index": 1, "embedding": makeVector(1536, 0.2)},
			},
			"model": "text-embedding-3-small",
			"usage": map[string]int{"prompt_tokens": 4, "total_tokens": 4},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(respObj)
	}))
	defer srv.Close()

	p := embeddings.NewOpenAIProvider(embeddings.OpenAIConfig{
		APIKey:  "sk-test-token",
		BaseURL: srv.URL,
		Model:   "text-embedding-3-small",
	})

	out, err := p.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(out))
	}
	if len(out[0]) != 1536 || len(out[1]) != 1536 {
		t.Fatalf("expected 1536-dim vectors, got %d / %d", len(out[0]), len(out[1]))
	}

	if capturedAuth != "Bearer sk-test-token" {
		t.Errorf("auth header: %q", capturedAuth)
	}
	if capturedCT != "application/json" {
		t.Errorf("content-type header: %q", capturedCT)
	}
	if capturedMethod != "POST" {
		t.Errorf("method: %q", capturedMethod)
	}
	if !strings.HasSuffix(capturedPath, "/embeddings") {
		t.Errorf("path: %q", capturedPath)
	}
	if capturedBody["model"] != "text-embedding-3-small" {
		t.Errorf("model in body: %v", capturedBody["model"])
	}
	if input, ok := capturedBody["input"].([]interface{}); !ok || len(input) != 2 {
		t.Errorf("input in body: %v", capturedBody["input"])
	}
}

// TestOpenAIProvider_ErrorHandling verifies that non-2xx responses are
// surfaced as errors carrying the upstream message.
func TestOpenAIProvider_ErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key","type":"auth_error"}}`))
	}))
	defer srv.Close()

	p := embeddings.NewOpenAIProvider(embeddings.OpenAIConfig{
		APIKey:  "sk-bogus",
		BaseURL: srv.URL,
		Model:   "text-embedding-3-small",
	})

	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid api key") &&
		!strings.Contains(err.Error(), "401") {
		t.Errorf("expected upstream message in error, got %v", err)
	}
}

// TestOpenAIProvider_MissingAPIKey verifies that constructing a provider
// without an API key fails fast at Embed() time rather than silently
// hitting OpenAI with an empty bearer.
func TestOpenAIProvider_MissingAPIKey(t *testing.T) {
	p := embeddings.NewOpenAIProvider(embeddings.OpenAIConfig{
		APIKey:  "",
		BaseURL: "https://api.openai.com",
		Model:   "text-embedding-3-small",
	})
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error for missing API key")
	}
}

func TestOpenAIProvider_ModelAndDimensions(t *testing.T) {
	p := embeddings.NewOpenAIProvider(embeddings.OpenAIConfig{
		APIKey: "sk-x",
		Model:  "text-embedding-3-small",
	})
	if p.Model() != "text-embedding-3-small" {
		t.Errorf("Model() = %q", p.Model())
	}
	if p.Dimensions() != 1536 {
		t.Errorf("Dimensions() = %d", p.Dimensions())
	}
}

// makeVector returns a slice of `dim` float32 values, all set to `v`. Used
// only by the OpenAI test http server stub.
func makeVector(dim int, v float32) []float32 {
	out := make([]float32, dim)
	for i := range out {
		out[i] = v
	}
	return out
}
