package embeddings_test

import (
	"context"
	"testing"

	"github.com/liyang/weave/pkg/embeddings"
)

// TestMockProvider_DeterministicOutput verifies that the mock provider
// returns the SAME embedding for the SAME input across calls. This is the
// property that lets tests assert exact distance values without flake.
func TestMockProvider_DeterministicOutput(t *testing.T) {
	p := embeddings.NewMockProvider()

	out1, err := p.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	out2, err := p.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatalf("second Embed: %v", err)
	}
	if len(out1) != 1 || len(out2) != 1 {
		t.Fatalf("expected len=1, got %d / %d", len(out1), len(out2))
	}
	if len(out1[0]) != p.Dimensions() {
		t.Fatalf("expected dim=%d, got %d", p.Dimensions(), len(out1[0]))
	}
	for i := range out1[0] {
		if out1[0][i] != out2[0][i] {
			t.Fatalf("non-deterministic at index %d: %v vs %v", i, out1[0][i], out2[0][i])
		}
	}
}

// TestMockProvider_BatchEqualsSingle verifies that calling Embed with a
// batch of N inputs is equivalent to N single-input calls. This guards
// against accidental cross-text contamination in the hash function.
func TestMockProvider_BatchEqualsSingle(t *testing.T) {
	p := embeddings.NewMockProvider()

	inputs := []string{"alpha", "beta", "gamma"}
	batch, err := p.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("batch Embed: %v", err)
	}
	if len(batch) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(batch))
	}

	for i, in := range inputs {
		single, err := p.Embed(context.Background(), []string{in})
		if err != nil {
			t.Fatalf("single Embed %d: %v", i, err)
		}
		if len(single) != 1 {
			t.Fatalf("expected single len=1, got %d", len(single))
		}
		for j := range single[0] {
			if single[0][j] != batch[i][j] {
				t.Fatalf("mismatch input %d index %d: %v vs %v", i, j, single[0][j], batch[i][j])
			}
		}
	}
}

// TestMockProvider_DifferentInputsDifferOutputs verifies that distinct
// inputs produce distinct vectors. (A trivially zero provider would pass
// the determinism test but be useless for nearest-neighbor ranking.)
func TestMockProvider_DifferentInputsDifferOutputs(t *testing.T) {
	p := embeddings.NewMockProvider()

	out, err := p.Embed(context.Background(), []string{"foo", "bar"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
	same := true
	for i := range out[0] {
		if out[0][i] != out[1][i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("expected different inputs to produce different vectors")
	}
}

func TestMockProvider_EmptyInput(t *testing.T) {
	p := embeddings.NewMockProvider()
	out, err := p.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("nil input: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil empty slice")
	}
	if len(out) != 0 {
		t.Fatalf("expected len 0, got %d", len(out))
	}
}

func TestMockProvider_ModelAndDimensions(t *testing.T) {
	p := embeddings.NewMockProvider()
	if p.Model() == "" {
		t.Error("expected non-empty Model()")
	}
	if p.Dimensions() != 1536 {
		t.Errorf("expected Dimensions=1536, got %d", p.Dimensions())
	}
}
