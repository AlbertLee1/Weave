package rag

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// fixedEmbedder returns a hand-tuned vector for each known text; everything
// else falls back to a deterministic "noise" vector. The dimensionality is
// intentionally small (3) so the cosine-similarity expectations in tests are
// trivial to reason about by hand.
type fixedEmbedder struct {
	model   string
	vectors map[string][]float32
}

func (f *fixedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		if v, ok := f.vectors[t]; ok {
			out[i] = append([]float32(nil), v...)
			continue
		}
		out[i] = []float32{0, 0, 1} // orthogonal-ish fallback
	}
	return out, nil
}

func (f *fixedEmbedder) Model() string { return f.model }

func (f *fixedEmbedder) Dimensions() int { return 3 }

// fakeExecutor returns a fixed objectset.Result. Errs when err is non-nil.
type fakeExecutor struct {
	result *objectset.Result
	err    error
	calls  int
}

func (f *fakeExecutor) Execute(_ context.Context, _ *objectset.Definition) (*objectset.Result, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// fakeLoader returns a preset slice of Documents and ignores the inputs.
type fakeLoader struct {
	docs     []Document
	err      error
	calls    int
	gotPKs   []string
	gotType  string
	gotOntol string
}

func (f *fakeLoader) LoadDocuments(_ context.Context, ontology, objectType string, pks []string) ([]Document, error) {
	f.calls++
	f.gotOntol = ontology
	f.gotType = objectType
	f.gotPKs = append([]string(nil), pks...)
	if f.err != nil {
		return nil, f.err
	}
	return f.docs, nil
}

func TestRetriever_RanksByCosineSimilarity(t *testing.T) {
	emb := &fixedEmbedder{
		model: "test-embedder",
		vectors: map[string][]float32{
			"who is alice":    {1, 0, 0},
			"alice is a hero": {0.95, 0.05, 0},
			"bob plays bass":  {0, 1, 0},
			"random trivia":   {0, 0, 1},
		},
	}
	exec := &fakeExecutor{
		result: &objectset.Result{
			ObjectType:  "character",
			PrimaryKeys: []string{"alice", "bob", "random"},
			Truncated:   false,
		},
	}
	loader := &fakeLoader{
		docs: []Document{
			{ObjectType: "character", PrimaryKey: "alice", Title: "Alice", Text: "alice is a hero"},
			{ObjectType: "character", PrimaryKey: "bob", Title: "Bob", Text: "bob plays bass"},
			{ObjectType: "character", PrimaryKey: "random", Title: "Random", Text: "random trivia"},
		},
	}

	r := NewRetriever(exec, emb, loader)
	res, err := r.Retrieve(context.Background(), RetrieveRequest{
		Query:     "who is alice",
		Ontology:  "northwind",
		Candidate: &objectset.Definition{Type: "base", ObjectType: "character"},
		K:         2,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("executor calls = %d, want 1", exec.calls)
	}
	if loader.calls != 1 {
		t.Fatalf("loader calls = %d, want 1", loader.calls)
	}
	if loader.gotType != "character" {
		t.Fatalf("loader objectType = %q, want %q", loader.gotType, "character")
	}
	if loader.gotOntol != "northwind" {
		t.Fatalf("loader ontology = %q, want %q", loader.gotOntol, "northwind")
	}
	if got, want := len(res.Matches), 2; got != want {
		t.Fatalf("matches = %d, want %d", got, want)
	}
	if res.Matches[0].Document.PrimaryKey != "alice" {
		t.Fatalf("top match = %q, want alice", res.Matches[0].Document.PrimaryKey)
	}
	if res.Matches[1].Document.PrimaryKey == "alice" {
		t.Fatalf("second match must not be alice (already rank 1)")
	}
	if res.Matches[0].Score <= res.Matches[1].Score {
		t.Fatalf("scores not ordered desc: %v", res.Matches)
	}
	if res.Truncated {
		t.Fatalf("truncated = true, want false")
	}
}

func TestRetriever_DefaultK(t *testing.T) {
	emb := &fixedEmbedder{model: "m", vectors: map[string][]float32{}}
	exec := &fakeExecutor{result: &objectset.Result{
		ObjectType:  "doc",
		PrimaryKeys: []string{"a", "b", "c", "d", "e", "f", "g", "h"},
	}}
	docs := make([]Document, 0, 8)
	for _, k := range exec.result.PrimaryKeys {
		docs = append(docs, Document{ObjectType: "doc", PrimaryKey: k, Text: k})
	}
	loader := &fakeLoader{docs: docs}
	r := NewRetriever(exec, emb, loader)

	res, err := r.Retrieve(context.Background(), RetrieveRequest{
		Query:     "anything",
		Candidate: &objectset.Definition{Type: "base", ObjectType: "doc"},
		// K omitted -> default
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got, want := len(res.Matches), DefaultK; got != want {
		t.Fatalf("default top-K = %d, want %d", got, want)
	}
}

func TestRetriever_PropagatesTruncated(t *testing.T) {
	emb := &fixedEmbedder{model: "m"}
	exec := &fakeExecutor{result: &objectset.Result{
		ObjectType:  "doc",
		PrimaryKeys: []string{"a"},
		Truncated:   true,
	}}
	loader := &fakeLoader{docs: []Document{{ObjectType: "doc", PrimaryKey: "a", Text: "a"}}}
	r := NewRetriever(exec, emb, loader)
	res, err := r.Retrieve(context.Background(), RetrieveRequest{
		Query:     "q",
		Candidate: &objectset.Definition{Type: "base", ObjectType: "doc"},
		K:         3,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if !res.Truncated {
		t.Fatalf("truncated not propagated")
	}
}

func TestRetriever_EmptyCandidateShortCircuits(t *testing.T) {
	emb := &fixedEmbedder{model: "m"}
	exec := &fakeExecutor{result: &objectset.Result{
		ObjectType:  "doc",
		PrimaryKeys: nil,
	}}
	loader := &fakeLoader{}
	r := NewRetriever(exec, emb, loader)
	res, err := r.Retrieve(context.Background(), RetrieveRequest{
		Query:     "q",
		Candidate: &objectset.Definition{Type: "base", ObjectType: "doc"},
		K:         5,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(res.Matches) != 0 {
		t.Fatalf("matches = %d, want 0", len(res.Matches))
	}
	if loader.calls != 0 {
		t.Fatalf("loader called %d times, want 0", loader.calls)
	}
}

func TestRetriever_SkipsEmptyDocumentText(t *testing.T) {
	emb := &fixedEmbedder{
		model: "m",
		vectors: map[string][]float32{
			"q":     {1, 0, 0},
			"alice": {1, 0, 0},
		},
	}
	exec := &fakeExecutor{result: &objectset.Result{
		ObjectType:  "doc",
		PrimaryKeys: []string{"alice", "blank"},
	}}
	loader := &fakeLoader{docs: []Document{
		{ObjectType: "doc", PrimaryKey: "alice", Text: "alice"},
		{ObjectType: "doc", PrimaryKey: "blank", Text: "   "},
	}}
	r := NewRetriever(exec, emb, loader)
	res, err := r.Retrieve(context.Background(), RetrieveRequest{
		Query:     "q",
		Candidate: &objectset.Definition{Type: "base", ObjectType: "doc"},
		K:         5,
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if got, want := len(res.Matches), 1; got != want {
		t.Fatalf("matches = %d, want %d (blank should be skipped)", got, want)
	}
	if res.Matches[0].Document.PrimaryKey != "alice" {
		t.Fatalf("kept doc = %q, want alice", res.Matches[0].Document.PrimaryKey)
	}
}

func TestRetriever_RejectsEmptyQuery(t *testing.T) {
	r := NewRetriever(&fakeExecutor{}, &fixedEmbedder{model: "m"}, &fakeLoader{})
	_, err := r.Retrieve(context.Background(), RetrieveRequest{Query: "  "})
	if err == nil || !strings.Contains(err.Error(), "query") {
		t.Fatalf("expected empty-query error, got %v", err)
	}
}

func TestRetriever_RejectsNilCandidate(t *testing.T) {
	r := NewRetriever(&fakeExecutor{}, &fixedEmbedder{model: "m"}, &fakeLoader{})
	_, err := r.Retrieve(context.Background(), RetrieveRequest{Query: "q"})
	if err == nil || !strings.Contains(err.Error(), "candidate") {
		t.Fatalf("expected nil-candidate error, got %v", err)
	}
}

func TestRetriever_RejectsNegativeK(t *testing.T) {
	r := NewRetriever(&fakeExecutor{}, &fixedEmbedder{model: "m"}, &fakeLoader{})
	_, err := r.Retrieve(context.Background(), RetrieveRequest{
		Query:     "q",
		Candidate: &objectset.Definition{Type: "base", ObjectType: "x"},
		K:         -1,
	})
	if err == nil || !strings.Contains(err.Error(), "K") {
		t.Fatalf("expected K validation error, got %v", err)
	}
}

func TestRetriever_PropagatesExecutorError(t *testing.T) {
	wantErr := errors.New("boom")
	r := NewRetriever(&fakeExecutor{err: wantErr}, &fixedEmbedder{model: "m"}, &fakeLoader{})
	_, err := r.Retrieve(context.Background(), RetrieveRequest{
		Query:     "q",
		Candidate: &objectset.Definition{Type: "base", ObjectType: "x"},
		K:         3,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wraps %v", err, wantErr)
	}
}

func TestRetriever_PropagatesLoaderError(t *testing.T) {
	wantErr := errors.New("loader exploded")
	exec := &fakeExecutor{result: &objectset.Result{
		ObjectType:  "doc",
		PrimaryKeys: []string{"a"},
	}}
	r := NewRetriever(exec, &fixedEmbedder{model: "m"}, &fakeLoader{err: wantErr})
	_, err := r.Retrieve(context.Background(), RetrieveRequest{
		Query:     "q",
		Candidate: &objectset.Definition{Type: "base", ObjectType: "doc"},
		K:         3,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want wraps %v", err, wantErr)
	}
}

func TestNewRetriever_NilDepsRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		deps func() (CandidateExecutor, Embedder, CorpusLoader)
	}{
		{"nil executor", func() (CandidateExecutor, Embedder, CorpusLoader) {
			return nil, &fixedEmbedder{model: "m"}, &fakeLoader{}
		}},
		{"nil embedder", func() (CandidateExecutor, Embedder, CorpusLoader) {
			return &fakeExecutor{}, nil, &fakeLoader{}
		}},
		{"nil loader", func() (CandidateExecutor, Embedder, CorpusLoader) {
			return &fakeExecutor{}, &fixedEmbedder{model: "m"}, nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic from NewRetriever with nil dep")
				}
			}()
			ex, em, ld := tc.deps()
			_ = NewRetriever(ex, em, ld)
		})
	}
}

func TestObjectSetExecutorImplementsCandidateExecutor(t *testing.T) {
	// Compile-time check: *objectset.Executor satisfies CandidateExecutor.
	var _ CandidateExecutor = (*objectset.Executor)(nil)
}
