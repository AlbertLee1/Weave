package objectset_test

import (
	"context"
	"errors"
	"testing"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// fakeVectorStore captures the last query and returns canned matches.
type fakeVectorStore struct {
	matches []objectset.NearestNeighborMatch
	err     error

	lastQuery objectset.NNVectorQuery
	calls     int
}

func (f *fakeVectorStore) FindNearestNeighbors(ctx context.Context, q objectset.NNVectorQuery) ([]objectset.NearestNeighborMatch, error) {
	f.calls++
	f.lastQuery = q
	if f.err != nil {
		return nil, f.err
	}
	return f.matches, nil
}

// fakeEmbeddingProvider returns a fixed vector regardless of input text.
type fakeEmbeddingProvider struct {
	vector []float32
	model  string
	err    error
	calls  int
	last   []string
}

func (f *fakeEmbeddingProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	f.calls++
	f.last = texts
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = f.vector
	}
	return out, nil
}

func (f *fakeEmbeddingProvider) Model() string { return f.model }

// TestExecuteNearestNeighbors_VectorQuery covers the happy path: an explicit
// query vector flows through to the VectorStore and the returned matches are
// returned in order.
func TestExecuteNearestNeighbors_VectorQuery(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	store := &fakeVectorStore{
		matches: []objectset.NearestNeighborMatch{
			{PrimaryKey: "e2", Distance: 0.10},
			{PrimaryKey: "e1", Distance: 0.22},
		},
	}
	executor.SetVectorStore(store)

	k := 2
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifier: &objectset.PropertyIdentifier{
			Property: struct {
				APIName string `json:"apiName"`
			}{APIName: "embedding"},
		},
		NumNeighbors: &k,
		Query: &objectset.NNQuery{
			Vector: &objectset.VectorQuery{Value: []float64{0.1, 0.2, 0.3}},
		},
	}

	res, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.ObjectType != "employee" {
		t.Errorf("ObjectType = %q, want employee", res.ObjectType)
	}
	if len(res.PrimaryKeys) != 2 || res.PrimaryKeys[0] != "e2" || res.PrimaryKeys[1] != "e1" {
		t.Errorf("PrimaryKeys = %v, want [e2 e1]", res.PrimaryKeys)
	}
	if store.calls != 1 {
		t.Errorf("store.calls = %d, want 1", store.calls)
	}
	if store.lastQuery.ObjectType != "employee" {
		t.Errorf("lastQuery.ObjectType = %q", store.lastQuery.ObjectType)
	}
	if store.lastQuery.K != 2 {
		t.Errorf("lastQuery.K = %d, want 2", store.lastQuery.K)
	}
	if len(store.lastQuery.QueryVector) != 3 {
		t.Errorf("lastQuery.QueryVector len = %d, want 3", len(store.lastQuery.QueryVector))
	}
	if store.lastQuery.PropertyAPIName != "embedding" {
		t.Errorf("lastQuery.PropertyAPIName = %q", store.lastQuery.PropertyAPIName)
	}
	// Candidate PKs from the inner base set should be propagated so the
	// vector store can scope its search to that subset.
	if len(store.lastQuery.CandidatePKs) != 4 {
		t.Errorf("CandidatePKs len = %d, want 4 (all 4 employees)", len(store.lastQuery.CandidatePKs))
	}
}

// TestExecuteNearestNeighbors_TextQuery exercises the text→vector path: the
// executor must use its EmbeddingProvider to embed the query text, then call
// the VectorStore with the resulting vector.
func TestExecuteNearestNeighbors_TextQuery(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	provider := &fakeEmbeddingProvider{
		vector: []float32{0.5, 0.5, 0.5},
		model:  "test-model-v1",
	}
	store := &fakeVectorStore{
		matches: []objectset.NearestNeighborMatch{
			{PrimaryKey: "e3", Distance: 0.05},
		},
	}
	executor.SetEmbeddingProvider(provider)
	executor.SetVectorStore(store)

	k := 1
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifier: &objectset.PropertyIdentifier{
			Property: struct {
				APIName string `json:"apiName"`
			}{APIName: "embedding"},
		},
		NumNeighbors: &k,
		Query: &objectset.NNQuery{
			Text: &objectset.TextQuery{Value: "find me sales people"},
		},
	}

	res, err := executor.Execute(context.Background(), def)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if provider.calls != 1 {
		t.Errorf("provider.calls = %d, want 1", provider.calls)
	}
	if len(provider.last) != 1 || provider.last[0] != "find me sales people" {
		t.Errorf("provider.last = %v", provider.last)
	}
	if store.lastQuery.Model != "test-model-v1" {
		t.Errorf("lastQuery.Model = %q, want test-model-v1", store.lastQuery.Model)
	}
	if len(res.PrimaryKeys) != 1 || res.PrimaryKeys[0] != "e3" {
		t.Errorf("PrimaryKeys = %v, want [e3]", res.PrimaryKeys)
	}
}

// TestExecuteNearestNeighbors_NoVectorStore: when no VectorStore is wired the
// executor must return a clear "not configured" error so callers can surface
// the misconfiguration.
func TestExecuteNearestNeighbors_NoVectorStore(t *testing.T) {
	executor, _ := setupExecutorTest(t)

	k := 5
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifier: &objectset.PropertyIdentifier{
			Property: struct {
				APIName string `json:"apiName"`
			}{APIName: "embedding"},
		},
		NumNeighbors: &k,
		Query:        &objectset.NNQuery{Vector: &objectset.VectorQuery{Value: []float64{0.1}}},
	}
	_, err := executor.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected error when no vector store configured")
	}
}

// TestExecuteNearestNeighbors_TextQueryNoProvider: text query path without
// an embedding provider must error rather than silently calling the store
// with an empty vector.
func TestExecuteNearestNeighbors_TextQueryNoProvider(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	executor.SetVectorStore(&fakeVectorStore{})

	k := 5
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifier: &objectset.PropertyIdentifier{
			Property: struct {
				APIName string `json:"apiName"`
			}{APIName: "embedding"},
		},
		NumNeighbors: &k,
		Query: &objectset.NNQuery{
			Text: &objectset.TextQuery{Value: "anything"},
		},
	}
	_, err := executor.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected error when text query has no embedding provider")
	}
}

// TestExecuteNearestNeighbors_PropagatesStoreError ensures backend failures
// flow up rather than being swallowed.
func TestExecuteNearestNeighbors_PropagatesStoreError(t *testing.T) {
	executor, _ := setupExecutorTest(t)
	store := &fakeVectorStore{err: errors.New("pgvector boom")}
	executor.SetVectorStore(store)

	k := 5
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: "employee",
		},
		PropertyIdentifier: &objectset.PropertyIdentifier{
			Property: struct {
				APIName string `json:"apiName"`
			}{APIName: "embedding"},
		},
		NumNeighbors: &k,
		Query:        &objectset.NNQuery{Vector: &objectset.VectorQuery{Value: []float64{0.1}}},
	}
	_, err := executor.Execute(context.Background(), def)
	if err == nil {
		t.Fatal("expected error from store to propagate")
	}
}
