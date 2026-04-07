package embeddings

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// mockDimensions is fixed to 1536 to match the production schema and the
// OpenAI ada-002 / text-embedding-3-small models. Tests that need a smaller
// vector should still use 1536 so the same code path is exercised.
const mockDimensions = 1536

// mockModel is the identifier returned by MockProvider.Model(). It is
// distinct from any real model name so production data and test data
// cannot be mixed up by accident.
const mockModel = "weave-mock-embedding-v1"

// MockProvider returns deterministic, hash-derived embeddings for tests
// without making any network calls. The same input always produces the
// same output and different inputs produce different outputs (with very
// high probability), but the values themselves are NOT semantically
// meaningful — they only support equality and ranking checks in tests.
type MockProvider struct{}

// NewMockProvider constructs a MockProvider. It takes no arguments because
// the mock is fully deterministic and stateless.
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

// Model returns the mock model identifier.
func (p *MockProvider) Model() string { return mockModel }

// Dimensions returns the fixed 1536-dimensional vector size.
func (p *MockProvider) Dimensions() int { return mockDimensions }

// Embed returns one deterministic vector per input text. Each vector is
// derived by repeatedly hashing the SHA-256 of the text combined with a
// counter, then unpacking the bytes as float32 values normalised to the
// open interval (-1, 1).
//
// The implementation is intentionally simple — it does not aim to produce
// realistic embeddings, only repeatable ones.
func (p *MockProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = mockVectorForText(t)
	}
	return out, nil
}

// mockVectorForText is the deterministic hash → vector function used by
// MockProvider. Pulled out into a package-private function so we can
// reuse it from internal benchmarks if we ever add them.
func mockVectorForText(text string) []float32 {
	vec := make([]float32, mockDimensions)
	// We need 4 bytes per dimension; sha256 yields 32 bytes per round.
	// 1536 / (32/4) = 192 rounds.
	const bytesPerDim = 4
	const hashBytes = 32
	const dimsPerHash = hashBytes / bytesPerDim // 8
	rounds := mockDimensions / dimsPerHash      // 192

	var counterBuf [8]byte
	for round := 0; round < rounds; round++ {
		binary.BigEndian.PutUint64(counterBuf[:], uint64(round))
		h := sha256.New()
		h.Write([]byte(text))
		h.Write(counterBuf[:])
		sum := h.Sum(nil)
		for j := 0; j < dimsPerHash; j++ {
			u := binary.BigEndian.Uint32(sum[j*bytesPerDim : (j+1)*bytesPerDim])
			// Map uint32 -> (-1, 1) float32 deterministically.
			f := float32(u)/float32(math.MaxUint32)*2 - 1
			vec[round*dimsPerHash+j] = f
		}
	}
	return vec
}
