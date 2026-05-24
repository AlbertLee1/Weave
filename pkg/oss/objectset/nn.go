package objectset

import (
	"context"
	"fmt"
	"sort"
)

// NNVectorQuery is the request the executor passes to a NNVectorStore. It
// carries the resolved query vector together with the metadata the store
// needs to scope its kNN search.
//
// CandidatePKs is the result of executing the inner ObjectSet — the store
// must restrict its results to this subset so the search respects ObjectSet
// composition. An empty slice means "no candidates" (the executor returns an
// empty result without ever calling the store).
type NNVectorQuery struct {
	Ontology        string
	ObjectType      string
	PropertyAPIName string
	QueryVector     []float32
	K               int
	CandidatePKs    []string
	Model           string
}

// NearestNeighborMatch is one row of an nearest-neighbours search. Distance
// is the cosine distance between the query vector and the stored embedding.
type NearestNeighborMatch struct {
	PrimaryKey string
	Distance   float32
}

// NNVectorStore is the executor's view of the vector backend. The production
// implementation wraps oms.Repository.FindNearestNeighbors against pgvector;
// tests inject a fake.
type NNVectorStore interface {
	FindNearestNeighbors(ctx context.Context, q NNVectorQuery) ([]NearestNeighborMatch, error)
}

// NNEmbeddingProvider produces query-time embeddings for the text branch of
// an NNQuery. Implementations are expected to be safe for concurrent use.
type NNEmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
}

// SetVectorStore wires an optional NNVectorStore so the executor can evaluate
// nearestNeighbors definitions. Without it, nearestNeighbors errors at
// execute time.
func (e *Executor) SetVectorStore(s NNVectorStore) { e.vectorStore = s }

// SetEmbeddingProvider wires an optional NNEmbeddingProvider so the executor
// can resolve text-only NN queries to a vector. Definitions that use the
// vector branch directly do not require a provider.
func (e *Executor) SetEmbeddingProvider(p NNEmbeddingProvider) { e.embedProvider = p }

// executeNearestNeighbors evaluates a nearestNeighbors ObjectSet definition.
// Steps:
//  1. Execute the inner ObjectSet to obtain the candidate primary keys.
//  2. Resolve the query vector — either supplied directly or by embedding the
//     query text via the configured provider.
//  3. Delegate to the vector store, scoping the search to the candidate set.
//  4. Return the matches as a Result keyed off the inner set's ObjectType.
func (e *Executor) executeNearestNeighbors(ctx context.Context, def *Definition) (*Result, error) {
	if e.vectorStore == nil {
		return nil, fmt.Errorf("nearestNeighbors: vector store not configured")
	}

	inner, err := e.execute(ctx, def.ObjectSet)
	if err != nil {
		return nil, fmt.Errorf("execute nearestNeighbors inner: %w", err)
	}

	// Empty candidate set short-circuits without consulting the store.
	if len(inner.PrimaryKeys) == 0 {
		return &Result{
			ObjectType:  inner.ObjectType,
			PrimaryKeys: nil,
			Truncated:   inner.Truncated,
		}, nil
	}

	queryVec, err := e.resolveNNQuery(ctx, def.Query)
	if err != nil {
		return nil, err
	}

	k := 10
	if def.NumNeighbors != nil && *def.NumNeighbors > 0 {
		k = *def.NumNeighbors
	}

	model := ""
	if e.embedProvider != nil {
		model = e.embedProvider.Model()
	}

	// Build the per-column dispatch list. The singular field and
	// plural list are mutually exclusive (enforced by Validate) and
	// at least one must be set, so this list has 1+ entries.
	propAPINames := nnPropertyAPINames(def)

	baseQ := NNVectorQuery{
		Ontology:     OntologyScopeFromContextOrEmpty(ctx),
		ObjectType:   inner.ObjectType,
		QueryVector:  queryVec,
		K:            k,
		CandidatePKs: inner.PrimaryKeys,
		Model:        model,
	}

	// Single-column fast path preserves byte-for-byte the legacy
	// behaviour — same store call shape, same ordering.
	if len(propAPINames) == 1 {
		q := baseQ
		q.PropertyAPIName = propAPINames[0]
		matches, err := e.vectorStore.FindNearestNeighbors(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("nearestNeighbors store: %w", err)
		}
		pks := make([]string, 0, len(matches))
		for _, m := range matches {
			pks = append(pks, m.PrimaryKey)
		}
		return &Result{
			ObjectType:  inner.ObjectType,
			PrimaryKeys: pks,
			Truncated:   inner.Truncated,
		}, nil
	}

	// Multi-column path: one store call per column, then fuse by
	// keeping the minimum distance per PK. A PK that ranks well on
	// any single column will float to the top of the fused list.
	bestDistance := make(map[string]float32, len(inner.PrimaryKeys))
	for _, prop := range propAPINames {
		q := baseQ
		q.PropertyAPIName = prop
		matches, err := e.vectorStore.FindNearestNeighbors(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("nearestNeighbors store (column %q): %w", prop, err)
		}
		for _, m := range matches {
			if prev, seen := bestDistance[m.PrimaryKey]; !seen || m.Distance < prev {
				bestDistance[m.PrimaryKey] = m.Distance
			}
		}
	}

	fused := make([]NearestNeighborMatch, 0, len(bestDistance))
	for pk, d := range bestDistance {
		fused = append(fused, NearestNeighborMatch{PrimaryKey: pk, Distance: d})
	}
	// Ascending distance — lower is closer. Ties broken on PK so the
	// ordering is deterministic across runs.
	sort.Slice(fused, func(i, j int) bool {
		if fused[i].Distance != fused[j].Distance {
			return fused[i].Distance < fused[j].Distance
		}
		return fused[i].PrimaryKey < fused[j].PrimaryKey
	})
	if len(fused) > k {
		fused = fused[:k]
	}

	pks := make([]string, 0, len(fused))
	for _, m := range fused {
		pks = append(pks, m.PrimaryKey)
	}
	return &Result{
		ObjectType:  inner.ObjectType,
		PrimaryKeys: pks,
		Truncated:   inner.Truncated,
	}, nil
}

// nnPropertyAPINames resolves the per-column dispatch list from either
// the singular PropertyIdentifier or the plural PropertyIdentifiers.
// Validate guarantees exactly one is populated; this helper just
// normalises both into a slice the executor can iterate over.
func nnPropertyAPINames(def *Definition) []string {
	if len(def.PropertyIdentifiers) > 0 {
		out := make([]string, 0, len(def.PropertyIdentifiers))
		for _, pi := range def.PropertyIdentifiers {
			out = append(out, pi.Property.APIName)
		}
		return out
	}
	if def.PropertyIdentifier != nil {
		return []string{def.PropertyIdentifier.Property.APIName}
	}
	return nil
}

// resolveNNQuery returns the float32 query vector for the given NNQuery.
// Either the vector branch or the text branch must be set; the text branch
// requires an embedding provider.
func (e *Executor) resolveNNQuery(ctx context.Context, q *NNQuery) ([]float32, error) {
	if q == nil {
		return nil, fmt.Errorf("nearestNeighbors: query is required")
	}
	if q.Vector != nil && len(q.Vector.Value) > 0 {
		out := make([]float32, len(q.Vector.Value))
		for i, v := range q.Vector.Value {
			out[i] = float32(v)
		}
		return out, nil
	}
	if q.Text != nil && q.Text.Value != "" {
		if e.embedProvider == nil {
			return nil, fmt.Errorf("nearestNeighbors: text query requires embedding provider")
		}
		vecs, err := e.embedProvider.Embed(ctx, []string{q.Text.Value})
		if err != nil {
			return nil, fmt.Errorf("embed query text: %w", err)
		}
		if len(vecs) != 1 {
			return nil, fmt.Errorf("embed query text: expected 1 vector, got %d", len(vecs))
		}
		return vecs[0], nil
	}
	return nil, fmt.Errorf("nearestNeighbors: query must specify vector or text")
}
