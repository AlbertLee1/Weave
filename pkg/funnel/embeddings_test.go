package funnel

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/liyang/weave/pkg/oms"
)

// fakeEmbedProvider returns a fixed-length deterministic vector and tracks
// every Embed call for assertions. It is safe for concurrent use because all
// state is guarded by a mutex.
type fakeEmbedProvider struct {
	mu     sync.Mutex
	calls  int
	last   []string
	model  string
	err    error
	dims   int
}

func (f *fakeEmbedProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.last = append([]string(nil), texts...)
	if f.err != nil {
		return nil, f.err
	}
	dims := f.dims
	if dims == 0 {
		dims = 4
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		v := make([]float32, dims)
		for j := range v {
			v[j] = float32(i + j + 1)
		}
		out[i] = v
	}
	return out, nil
}

func (f *fakeEmbedProvider) Model() string {
	if f.model == "" {
		return "fake-embed-v1"
	}
	return f.model
}

func (f *fakeEmbedProvider) Dimensions() int {
	if f.dims == 0 {
		return 4
	}
	return f.dims
}

// fakeEmbeddingStore captures every UpsertObjectEmbedding call.
type fakeEmbeddingStore struct {
	mu      sync.Mutex
	upserts []oms.ObjectEmbedding
	err     error
}

func (f *fakeEmbeddingStore) UpsertObjectEmbedding(_ context.Context, e *oms.ObjectEmbedding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.upserts = append(f.upserts, *e)
	return nil
}

func (f *fakeEmbeddingStore) snapshot() []oms.ObjectEmbedding {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]oms.ObjectEmbedding, len(f.upserts))
	copy(out, f.upserts)
	return out
}

// TestEmbeddingHook_GeneratesOnCreate covers the happy path: a CREATE edit
// triggers an embed + upsert with the correct fields.
func TestEmbeddingHook_GeneratesOnCreate(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	prov := &fakeEmbedProvider{}
	store := &fakeEmbeddingStore{}
	consumer.SetEmbeddingProvider(prov)
	consumer.SetEmbeddingStore(store)
	consumer.SetEmbeddingObjectTypes(map[string]EmbeddingConfig{
		"employee": {SourceProperties: []string{"name", "title"}},
	})

	batch := EditBatch{
		ID:              "b1",
		OntologyAPIName: testOntology,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-1",
				Properties: map[string]interface{}{
					"name":  "Alice Wong",
					"title": "Senior Engineer",
				},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("applyBatchWithHistory: %v", err)
	}

	if prov.calls != 1 {
		t.Errorf("provider.calls = %d, want 1", prov.calls)
	}
	if len(prov.last) != 1 {
		t.Fatalf("provider.last len = %d, want 1", len(prov.last))
	}
	if prov.last[0] == "" {
		t.Errorf("provider.last[0] empty")
	}

	rows := store.snapshot()
	if len(rows) != 1 {
		t.Fatalf("store rows = %d, want 1", len(rows))
	}
	if rows[0].PrimaryKey != "emp-1" {
		t.Errorf("PrimaryKey = %q", rows[0].PrimaryKey)
	}
	if rows[0].Model != "fake-embed-v1" {
		t.Errorf("Model = %q", rows[0].Model)
	}
	if len(rows[0].Embedding) == 0 {
		t.Errorf("Embedding empty")
	}
	if rows[0].SourceText == "" {
		t.Errorf("SourceText empty")
	}
}

// TestEmbeddingHook_SkipsDelete: DELETE edits must NOT trigger embedding
// generation since there is nothing to embed.
func TestEmbeddingHook_SkipsDelete(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	prov := &fakeEmbedProvider{}
	store := &fakeEmbeddingStore{}
	consumer.SetEmbeddingProvider(prov)
	consumer.SetEmbeddingStore(store)
	consumer.SetEmbeddingObjectTypes(map[string]EmbeddingConfig{
		"employee": {SourceProperties: []string{"name"}},
	})

	batch := EditBatch{
		ID:              "b2",
		OntologyAPIName: testOntology,
		Edits: []Edit{
			{Type: EditTypeDelete, ObjectType: "employee", PrimaryKey: "emp-2"},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("applyBatchWithHistory: %v", err)
	}
	if prov.calls != 0 {
		t.Errorf("provider.calls = %d, want 0 on DELETE", prov.calls)
	}
	if len(store.snapshot()) != 0 {
		t.Errorf("store rows = %d, want 0 on DELETE", len(store.snapshot()))
	}
}

// TestEmbeddingHook_SkipsUnconfiguredObjectType: object types that are not
// in the embedding config must be left alone (no embed call, no upsert).
func TestEmbeddingHook_SkipsUnconfiguredObjectType(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	prov := &fakeEmbedProvider{}
	store := &fakeEmbeddingStore{}
	consumer.SetEmbeddingProvider(prov)
	consumer.SetEmbeddingStore(store)
	// no SetEmbeddingObjectTypes call → no types configured

	batch := EditBatch{
		ID:              "b3",
		OntologyAPIName: testOntology,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-3",
				Properties: map[string]interface{}{"name": "Bob"},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("applyBatchWithHistory: %v", err)
	}
	if prov.calls != 0 {
		t.Errorf("provider.calls = %d, want 0 for unconfigured type", prov.calls)
	}
	if len(store.snapshot()) != 0 {
		t.Errorf("store rows = %d, want 0", len(store.snapshot()))
	}
}

// TestEmbeddingHook_DoesNotBlockOnProviderError: provider failures must be
// logged but never abort the index commit, since indexing is the source of
// truth for read paths.
func TestEmbeddingHook_DoesNotBlockOnProviderError(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	prov := &fakeEmbedProvider{err: errors.New("openai is down")}
	store := &fakeEmbeddingStore{}
	consumer.SetEmbeddingProvider(prov)
	consumer.SetEmbeddingStore(store)
	consumer.SetEmbeddingObjectTypes(map[string]EmbeddingConfig{
		"employee": {SourceProperties: []string{"name"}},
	})

	batch := EditBatch{
		ID:              "b4",
		OntologyAPIName: testOntology,
		Edits: []Edit{
			{
				Type:       EditTypeCreate,
				ObjectType: "employee",
				PrimaryKey: "emp-4",
				Properties: map[string]interface{}{"name": "Carol"},
			},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("applyBatchWithHistory must not fail on embed error: %v", err)
	}
	if len(store.snapshot()) != 0 {
		t.Errorf("store rows = %d, want 0 (provider failed)", len(store.snapshot()))
	}
}

// TestEmbeddingHook_RateLimited: when a token bucket allows N events per
// window and the batch produces > N edits, only N must reach the provider —
// the rest are skipped (logged as throttled). This proves the rate limiter
// is wired and not a no-op.
func TestEmbeddingHook_RateLimited(t *testing.T) {
	consumer, _ := setupTestConsumer(t)
	prov := &fakeEmbedProvider{}
	store := &fakeEmbeddingStore{}
	consumer.SetEmbeddingProvider(prov)
	consumer.SetEmbeddingStore(store)
	consumer.SetEmbeddingObjectTypes(map[string]EmbeddingConfig{
		"employee": {SourceProperties: []string{"name"}},
	})
	// 2 tokens per second, burst 2 — only the first two edits should be
	// embedded, the third is dropped.
	consumer.SetEmbeddingRateLimiter(rate.NewLimiter(rate.Limit(2), 2))

	batch := EditBatch{
		ID:              "b5",
		OntologyAPIName: testOntology,
		Edits: []Edit{
			{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "p1", Properties: map[string]interface{}{"name": "A"}},
			{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "p2", Properties: map[string]interface{}{"name": "B"}},
			{Type: EditTypeCreate, ObjectType: "employee", PrimaryKey: "p3", Properties: map[string]interface{}{"name": "C"}},
		},
	}
	if err := consumer.applyBatchWithHistory(context.Background(), batch); err != nil {
		t.Fatalf("applyBatchWithHistory: %v", err)
	}
	// allow no time for replenishment
	_ = time.Millisecond
	if prov.calls > 2 {
		t.Errorf("provider.calls = %d, want at most 2 (rate limited)", prov.calls)
	}
	if len(store.snapshot()) > 2 {
		t.Errorf("store rows = %d, want at most 2 (rate limited)", len(store.snapshot()))
	}
}
