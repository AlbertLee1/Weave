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

// TestSentenceTransformersProvider_EndpointSigning verifies the request
// shape posted to {BaseURL}/embed and that vectors return in input order.
func TestSentenceTransformersProvider_EndpointSigning(t *testing.T) {
	var capturedPath, capturedCT, capturedAuth string
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		capturedCT = r.Header.Get("Content-Type")
		capturedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &capturedBody)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"embeddings": [][]float32{
				makeVector(384, 0.1),
				makeVector(384, 0.2),
			},
			"model": "sentence-transformers/all-MiniLM-L6-v2",
		})
	}))
	defer srv.Close()

	p := embeddings.NewSentenceTransformersProvider(embeddings.SentenceTransformersConfig{
		BaseURL: srv.URL,
		APIKey:  "shim-token",
	})
	out, err := p.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(out) != 2 || len(out[0]) != 384 || len(out[1]) != 384 {
		t.Fatalf("unexpected vectors: len=%d, dims=%d/%d", len(out), len(out[0]), len(out[1]))
	}
	if !strings.HasSuffix(capturedPath, "/embed") {
		t.Errorf("path: %q", capturedPath)
	}
	if capturedCT != "application/json" {
		t.Errorf("content-type: %q", capturedCT)
	}
	if capturedAuth != "Bearer shim-token" {
		t.Errorf("auth header: %q", capturedAuth)
	}
	if texts, ok := capturedBody["texts"].([]interface{}); !ok || len(texts) != 2 {
		t.Errorf("texts in body: %v", capturedBody["texts"])
	}
	if capturedBody["model"] != "sentence-transformers/all-MiniLM-L6-v2" {
		t.Errorf("model in body: %v", capturedBody["model"])
	}
}

// TestSentenceTransformersProvider_FlexibleDimensions exercises a 768-dim
// configuration (e.g. all-mpnet-base-v2) to prove the provider isn't
// hard-coded to 384.
func TestSentenceTransformersProvider_FlexibleDimensions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"embeddings": [][]float32{makeVector(768, 0.5)},
		})
	}))
	defer srv.Close()

	p := embeddings.NewSentenceTransformersProvider(embeddings.SentenceTransformersConfig{
		BaseURL:    srv.URL,
		Model:      "sentence-transformers/all-mpnet-base-v2",
		Dimensions: 768,
	})
	if p.Dimensions() != 768 {
		t.Fatalf("Dimensions() = %d, want 768", p.Dimensions())
	}
	out, err := p.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(out[0]) != 768 {
		t.Fatalf("expected 768 dims, got %d", len(out[0]))
	}
}

// TestSentenceTransformersProvider_MissingBaseURL fails fast at Embed time
// when no shim URL is configured (no sensible default for an arbitrary
// inference backend).
func TestSentenceTransformersProvider_MissingBaseURL(t *testing.T) {
	p := embeddings.NewSentenceTransformersProvider(embeddings.SentenceTransformersConfig{})
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil || !strings.Contains(err.Error(), "BaseURL") {
		t.Fatalf("expected BaseURL error, got %v", err)
	}
}

// TestSentenceTransformersProvider_ErrorHandling surfaces non-2xx with
// the upstream body so operators see what the shim actually returned.
func TestSentenceTransformersProvider_ErrorHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail":"model not loaded"}`))
	}))
	defer srv.Close()

	p := embeddings.NewSentenceTransformersProvider(embeddings.SentenceTransformersConfig{
		BaseURL: srv.URL,
	})
	_, err := p.Embed(context.Background(), []string{"hello"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "model not loaded") {
		t.Errorf("expected 500 + body in error, got %v", err)
	}
}

// TestSentenceTransformersProvider_VectorCountMismatch rejects a response
// that drops or duplicates vectors relative to the input.
func TestSentenceTransformersProvider_VectorCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"embeddings": [][]float32{makeVector(384, 0.1)},
		})
	}))
	defer srv.Close()

	p := embeddings.NewSentenceTransformersProvider(embeddings.SentenceTransformersConfig{
		BaseURL: srv.URL,
	})
	_, err := p.Embed(context.Background(), []string{"a", "b"})
	if err == nil || !strings.Contains(err.Error(), "expected 2 vectors, got 1") {
		t.Fatalf("expected vector-count mismatch error, got %v", err)
	}
}

func TestSentenceTransformersProvider_ModelAndDimensionsDefaults(t *testing.T) {
	p := embeddings.NewSentenceTransformersProvider(embeddings.SentenceTransformersConfig{
		BaseURL: "http://unused.invalid",
	})
	if p.Model() != "sentence-transformers/all-MiniLM-L6-v2" {
		t.Errorf("Model() = %q", p.Model())
	}
	if p.Dimensions() != 384 {
		t.Errorf("Dimensions() = %d", p.Dimensions())
	}
}

func TestSentenceTransformersProvider_EmptyInput(t *testing.T) {
	p := embeddings.NewSentenceTransformersProvider(embeddings.SentenceTransformersConfig{
		BaseURL: "http://unused.invalid",
	})
	out, err := p.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("nil input: %v", err)
	}
	if out == nil || len(out) != 0 {
		t.Fatalf("expected empty non-nil slice, got %v", out)
	}
}
