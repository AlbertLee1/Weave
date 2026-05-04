package main

import (
	"testing"

	"github.com/liyang/weave/pkg/embeddings"
)

// TestBuildEmbeddingProvider_ExplicitMock proves that
// WEAVE_EMBED_PROVIDER=mock returns a working MockProvider regardless of
// any other env vars in the parent shell.
func TestBuildEmbeddingProvider_ExplicitMock(t *testing.T) {
	t.Setenv("WEAVE_EMBED_PROVIDER", "mock")
	t.Setenv("WEAVE_OPENAI_API_KEY", "should-be-ignored")

	got := buildEmbeddingProvider()
	if got == nil {
		t.Fatal("expected provider, got nil")
	}
	if _, ok := got.(*embeddings.MockProvider); !ok {
		t.Fatalf("expected *MockProvider, got %T", got)
	}
}

// TestBuildEmbeddingProvider_ExplicitOpenAI verifies the explicit selection
// path picks the OpenAI provider when WEAVE_EMBED_PROVIDER=openai and an
// API key is configured. Honours the legacy WEAVE_OPENAI_API_KEY name AND
// the conventional OPENAI_API_KEY fallback.
func TestBuildEmbeddingProvider_ExplicitOpenAI(t *testing.T) {
	t.Setenv("WEAVE_EMBED_PROVIDER", "openai")
	t.Setenv("WEAVE_OPENAI_API_KEY", "sk-test")
	t.Setenv("WEAVE_EMBED_MODEL", "text-embedding-3-small")

	got := buildEmbeddingProvider()
	if got == nil {
		t.Fatal("expected provider, got nil")
	}
	if _, ok := got.(*embeddings.OpenAIProvider); !ok {
		t.Fatalf("expected *OpenAIProvider, got %T", got)
	}
	if got.Dimensions() != 1536 {
		t.Errorf("Dimensions() = %d, want 1536", got.Dimensions())
	}
}

func TestBuildEmbeddingProvider_ExplicitOpenAI_MissingKey(t *testing.T) {
	t.Setenv("WEAVE_EMBED_PROVIDER", "openai")
	t.Setenv("WEAVE_OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	got := buildEmbeddingProvider()
	if got != nil {
		t.Fatalf("expected nil (disabled) when API key missing, got %T", got)
	}
}

func TestBuildEmbeddingProvider_ExplicitOllama(t *testing.T) {
	t.Setenv("WEAVE_EMBED_PROVIDER", "ollama")
	t.Setenv("WEAVE_EMBED_OLLAMA_BASE_URL", "http://ollama.local:11434")
	t.Setenv("WEAVE_EMBED_MODEL", "all-minilm")
	t.Setenv("WEAVE_EMBED_DIMENSIONS", "384")

	got := buildEmbeddingProvider()
	if got == nil {
		t.Fatal("expected provider, got nil")
	}
	if _, ok := got.(*embeddings.OllamaProvider); !ok {
		t.Fatalf("expected *OllamaProvider, got %T", got)
	}
	if got.Model() != "all-minilm" {
		t.Errorf("Model() = %q", got.Model())
	}
	if got.Dimensions() != 384 {
		t.Errorf("Dimensions() = %d, want 384", got.Dimensions())
	}
}

func TestBuildEmbeddingProvider_ExplicitOllama_Defaults(t *testing.T) {
	t.Setenv("WEAVE_EMBED_PROVIDER", "ollama")
	t.Setenv("WEAVE_EMBED_OLLAMA_BASE_URL", "")
	t.Setenv("WEAVE_EMBED_MODEL", "")
	t.Setenv("WEAVE_EMBED_DIMENSIONS", "")

	got := buildEmbeddingProvider()
	if got == nil {
		t.Fatal("expected provider, got nil")
	}
	if got.Model() != "nomic-embed-text" {
		t.Errorf("Model() = %q, want nomic-embed-text", got.Model())
	}
	if got.Dimensions() != 768 {
		t.Errorf("Dimensions() = %d, want 768", got.Dimensions())
	}
}

func TestBuildEmbeddingProvider_ExplicitSentenceTransformers(t *testing.T) {
	t.Setenv("WEAVE_EMBED_PROVIDER", "sentence_transformers")
	t.Setenv("WEAVE_EMBED_ST_BASE_URL", "http://st.local:8000")
	t.Setenv("WEAVE_EMBED_MODEL", "sentence-transformers/all-mpnet-base-v2")
	t.Setenv("WEAVE_EMBED_DIMENSIONS", "768")

	got := buildEmbeddingProvider()
	if got == nil {
		t.Fatal("expected provider, got nil")
	}
	if _, ok := got.(*embeddings.SentenceTransformersProvider); !ok {
		t.Fatalf("expected *SentenceTransformersProvider, got %T", got)
	}
	if got.Dimensions() != 768 {
		t.Errorf("Dimensions() = %d, want 768", got.Dimensions())
	}
	if got.Model() != "sentence-transformers/all-mpnet-base-v2" {
		t.Errorf("Model() = %q", got.Model())
	}
}

func TestBuildEmbeddingProvider_ExplicitST_HyphenAlias(t *testing.T) {
	// Hyphen and underscore should be interchangeable; verify the
	// case-folding + replace-all logic catches "sentence-transformers".
	t.Setenv("WEAVE_EMBED_PROVIDER", "Sentence-Transformers")
	t.Setenv("WEAVE_EMBED_ST_BASE_URL", "http://st.local:8000")

	got := buildEmbeddingProvider()
	if got == nil {
		t.Fatal("expected provider, got nil")
	}
	if _, ok := got.(*embeddings.SentenceTransformersProvider); !ok {
		t.Fatalf("expected *SentenceTransformersProvider, got %T", got)
	}
}

func TestBuildEmbeddingProvider_ExplicitST_MissingBaseURL(t *testing.T) {
	t.Setenv("WEAVE_EMBED_PROVIDER", "sentence_transformers")
	t.Setenv("WEAVE_EMBED_ST_BASE_URL", "")

	got := buildEmbeddingProvider()
	if got != nil {
		t.Fatalf("expected nil (disabled) when ST base URL missing, got %T", got)
	}
}

func TestBuildEmbeddingProvider_ExplicitUnknown(t *testing.T) {
	t.Setenv("WEAVE_EMBED_PROVIDER", "definitely-not-a-provider")

	got := buildEmbeddingProvider()
	if got != nil {
		t.Fatalf("expected nil for unknown provider, got %T", got)
	}
}

// TestBuildEmbeddingProvider_LegacyFallback ensures setups predating
// US-436 keep working: with WEAVE_EMBED_PROVIDER unset, WEAVE_EMBED_MOCK=1
// still wins over WEAVE_OPENAI_API_KEY and the OpenAI fallback fires when
// neither sentinel is set.
func TestBuildEmbeddingProvider_LegacyFallback(t *testing.T) {
	t.Setenv("WEAVE_EMBED_PROVIDER", "")

	t.Run("mock wins when WEAVE_EMBED_MOCK=1", func(t *testing.T) {
		t.Setenv("WEAVE_EMBED_MOCK", "1")
		t.Setenv("WEAVE_OPENAI_API_KEY", "sk-x")
		got := buildEmbeddingProvider()
		if _, ok := got.(*embeddings.MockProvider); !ok {
			t.Fatalf("expected MockProvider, got %T", got)
		}
	})
	t.Run("openai when only key set", func(t *testing.T) {
		t.Setenv("WEAVE_EMBED_MOCK", "")
		t.Setenv("WEAVE_OPENAI_API_KEY", "sk-x")
		got := buildEmbeddingProvider()
		if _, ok := got.(*embeddings.OpenAIProvider); !ok {
			t.Fatalf("expected OpenAIProvider, got %T", got)
		}
	})
	t.Run("nil when nothing set", func(t *testing.T) {
		t.Setenv("WEAVE_EMBED_MOCK", "")
		t.Setenv("WEAVE_OPENAI_API_KEY", "")
		got := buildEmbeddingProvider()
		if got != nil {
			t.Fatalf("expected nil, got %T", got)
		}
	})
}
