// Package embeddings defines the EmbeddingProvider abstraction used by the
// nearestNeighbors ObjectSet executor and any future vector-search consumers
// inside Weave. A Provider hides the underlying model (OpenAI, Cohere, a
// local sentence-transformer, ...) behind a uniform Embed/Model/Dimensions
// surface so the rest of the system can stay model-agnostic.
package embeddings

import "context"

// Provider produces vector embeddings for a batch of input texts.
//
// Implementations MUST be safe for concurrent use: the executor calls
// Embed from arbitrary HTTP request goroutines without external locking.
//
// The returned slice MUST have len(texts) entries in the same order as the
// input. Each inner slice MUST have exactly Dimensions() values. Empty input
// returns an empty (non-nil) slice and a nil error.
type Provider interface {
	// Embed returns the embedding vector for a batch of input texts.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Model returns the model identifier (e.g. "text-embedding-3-small").
	// Stored alongside each embedding so multiple models can coexist.
	Model() string

	// Dimensions returns the vector dimensionality of every Embed() output.
	// Common production values are 384 (MiniLM-style), 768 (mpnet / many
	// Ollama models), and 1536 (OpenAI ada-002 / text-embedding-3-small).
	// Storage backends (e.g. pgvector columns) are typed against a fixed
	// dimension, so the configured Provider.Dimensions() MUST match the
	// schema dim of the column it writes to or the insert will fail.
	Dimensions() int
}
