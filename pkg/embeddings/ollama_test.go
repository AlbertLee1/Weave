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

// TestOllamaProvider_EndpointSigning verifies the request shape sent to
// /api/embed and parses the embeddings array back in input order.
func TestOllamaProvider_EndpointSigning(t *testing.T) {
	var capturedPath, capturedMethod, capturedCT string
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		capturedCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"model": "nomic-embed-text",
			"embeddings": [][]float32{
				makeVector(768, 0.1),
				makeVector(768, 0.2),
			},
		})
	}))
	defer srv.Close()

	p := embeddings.NewOllamaProvider(embeddings.OllamaConfig{BaseURL: srv.URL})
	out, err := p.Embed(context.Background(), []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(out))
	}
	if len(out[0]) != 768 || len(out[1]) != 768 {
		t.Fatalf("expected 768 dims, got %d / %d", len(out[0]), len(out[1]))
	}
	if capturedMethod != http.MethodPost {
		t.Errorf("method: %q", capturedMethod)
	}
	if !strings.HasSuffix(capturedPath, "/api/embed") {
		t.Errorf("path: %q", capturedPath)
	}
	if capturedCT != "application/json" {
		t.Errorf("content-type: %q", capturedCT)
	}
	if capturedBody["model"] != "nomic-embed-text" {
		t.Errorf("model in body: %v", capturedBody["model"])
	}
	if input, ok := capturedBody["input"].([]interface{}); !ok || len(input) != 2 {
		t.Errorf("input in body: %v", capturedBody["input"])
	}
}

// TestOllamaProvider_FlexibleDimensions verifies the provider honours a
// non-default Dimensions configuration end-to-end.
func TestOllamaProvider_FlexibleDimensions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"embeddings": [][]float32{makeVector(384, 0.5)},
		})
	}))
	defer srv.Close()

	p := embeddings.NewOllamaProvider(embeddings.OllamaConfig{
		BaseURL:    srv.URL,
		Model:      "all-minilm",
		Dimensions: 384,
	})
	if p.Dimensions() != 384 {
		t.Fatalf("Dimensions() = %d, want 384", p.Dimensions())
	}
	out, err := p.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(out[0]) != 384 {
		t.Fatalf("expected 384 dims, got %d", len(out[0]))
	}
}

// TestOllamaProvider_DimensionsMismatch verifies the provider rejects a
// response whose vector length disagrees with the configured Dimensions.
func TestOllamaProvider_DimensionsMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"embeddings": [][]float32{makeVector(100, 0.1)},
		})
	}))
	defer srv.Close()

	p := embeddings.NewOllamaProvider(embeddings.OllamaConfig{
		BaseURL:    srv.URL,
		Dimensions: 768,
	})
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil || !strings.Contains(err.Error(), "100 dims") {
		t.Fatalf("expected dim mismatch error, got %v", err)
	}
}

// TestOllamaProvider_ErrorHandling surfaces non-2xx responses with the
// upstream body so operators can debug daemon / model issues.
func TestOllamaProvider_ErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`model "missing" not found`))
	}))
	defer srv.Close()

	p := embeddings.NewOllamaProvider(embeddings.OllamaConfig{
		BaseURL: srv.URL,
		Model:   "missing",
	})
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "missing") {
		t.Errorf("expected 404 + upstream body, got %v", err)
	}
}

func TestOllamaProvider_EmptyInput(t *testing.T) {
	p := embeddings.NewOllamaProvider(embeddings.OllamaConfig{BaseURL: "http://unused.invalid"})
	out, err := p.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("nil input: %v", err)
	}
	if out == nil || len(out) != 0 {
		t.Fatalf("expected empty non-nil slice, got %v", out)
	}
}

func TestOllamaProvider_ModelAndDimensionsDefaults(t *testing.T) {
	p := embeddings.NewOllamaProvider(embeddings.OllamaConfig{})
	if p.Model() != "nomic-embed-text" {
		t.Errorf("Model() = %q", p.Model())
	}
	if p.Dimensions() != 768 {
		t.Errorf("Dimensions() = %d", p.Dimensions())
	}
}
