// Package rag wires Weave's ObjectSet executor and embedding provider
// into a Semantic Retriever for the AI Platform (US-283). The Retriever
// resolves a candidate ObjectSet, fetches each candidate's document text
// through a CorpusLoader, embeds query + corpus, and returns the top-K
// matches ranked by cosine similarity. The result is intended to be
// rendered into a prompt-template {{context}} slot — see template.go.
package rag

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/liyang/weave/pkg/oss/objectset"
)

// DefaultK is the number of matches Retriever.Retrieve returns when
// RetrieveRequest.K is left at zero. Five is small enough to fit comfortably
// in any frontier-model context window and large enough to give the LLM a
// useful spread of candidates without diluting attention.
const DefaultK = 5

// CandidateExecutor narrows objectset.Executor to the single Execute
// method the retriever needs. Tests inject a fake; production wires
// *objectset.Executor directly.
type CandidateExecutor interface {
	Execute(ctx context.Context, def *objectset.Definition) (*objectset.Result, error)
}

// Embedder is the embedding-provider surface the retriever consumes. It
// matches embeddings.Provider exactly (Embed + Model) so production code
// can hand a *embeddings.OpenAI / *embeddings.MockProvider straight
// through. Defining a local alias keeps pkg/aip/rag from depending on
// pkg/embeddings just to name the interface.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
}

// CorpusLoader fetches the textual representation of every primary key
// in pks from objectType. Implementations typically read the indexed
// Bleve document and project a single text-bearing property — but the
// Retriever does not care HOW the text is materialised, only that the
// returned slice is in the same order as pks (or that each Document
// carries its own PrimaryKey so callers can re-align).
//
// The returned slice may be SHORTER than pks: if a primary key is
// missing or has no text, the loader is free to omit it and the
// retriever simply ranks fewer candidates.
type CorpusLoader interface {
	LoadDocuments(ctx context.Context, ontology, objectType string, pks []string) ([]Document, error)
}

// Document is one row of the retrieval corpus. Text is the body the
// retriever embeds for similarity scoring; Title is an optional short
// header rendered alongside Text in the prompt template.
type Document struct {
	ObjectType string
	PrimaryKey string
	Title      string
	Text       string
}

// RetrieveRequest is the input to Retriever.Retrieve.
type RetrieveRequest struct {
	// Query is the free-text input the LLM will eventually answer. It is
	// embedded once and compared against every candidate document.
	Query string

	// Ontology is the API name of the ontology the candidate ObjectSet
	// lives in. Forwarded to the loader so backends can scope their
	// reads. The executor itself reads ontology from the ctx scope, so
	// callers should typically wrap ctx with WithOntologyScope before
	// calling Retrieve.
	Ontology string

	// Candidate is the ObjectSet that bounds the corpus the retriever
	// considers. Required.
	Candidate *objectset.Definition

	// K is the number of matches to return. K=0 falls back to DefaultK;
	// a negative K is rejected with a validation error.
	K int
}

// Match is one row of the retrieval result. Score is the cosine
// similarity in [-1, 1]; higher is more similar.
type Match struct {
	Document Document
	Score    float32
}

// Result is the collection of top-K matches together with the candidate
// ObjectSet's truncation flag. Truncated propagates from the candidate
// execution so callers can warn the user when the corpus was capped at
// objectset.BaseExecutionCap before scoring.
type Result struct {
	Matches   []Match
	Truncated bool
}

// Retriever is the encapsulation of the Semantic-retriever flow:
// resolve candidate -> load corpus -> embed -> rank.
//
// All dependencies are required; passing nil to NewRetriever panics so
// the wiring path fails loudly at boot rather than at the first request.
type Retriever struct {
	executor CandidateExecutor
	embedder Embedder
	loader   CorpusLoader

	defaultK int
}

// NewRetriever constructs a Retriever. None of the dependencies may be
// nil — wiring sites pass concrete implementations or stubs. The default
// top-K is DefaultK; override with WithDefaultK.
func NewRetriever(executor CandidateExecutor, embedder Embedder, loader CorpusLoader) *Retriever {
	if executor == nil {
		panic("rag: NewRetriever: executor must not be nil")
	}
	if embedder == nil {
		panic("rag: NewRetriever: embedder must not be nil")
	}
	if loader == nil {
		panic("rag: NewRetriever: loader must not be nil")
	}
	return &Retriever{
		executor: executor,
		embedder: embedder,
		loader:   loader,
		defaultK: DefaultK,
	}
}

// WithDefaultK overrides the default top-K. Returns the receiver so the
// caller can chain construction.
func (r *Retriever) WithDefaultK(k int) *Retriever {
	if k > 0 {
		r.defaultK = k
	}
	return r
}

// Retrieve resolves req.Candidate, loads the candidate documents,
// embeds query + corpus, and returns the top-K matches sorted by
// descending cosine similarity.
func (r *Retriever) Retrieve(ctx context.Context, req RetrieveRequest) (*Result, error) {
	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("rag: query must not be empty")
	}
	if req.Candidate == nil {
		return nil, fmt.Errorf("rag: candidate ObjectSet is required")
	}
	if req.K < 0 {
		return nil, fmt.Errorf("rag: K must be >= 0 (got %d)", req.K)
	}
	k := req.K
	if k == 0 {
		k = r.defaultK
	}

	candRes, err := r.executor.Execute(ctx, req.Candidate)
	if err != nil {
		return nil, fmt.Errorf("rag: execute candidate: %w", err)
	}
	if candRes == nil || len(candRes.PrimaryKeys) == 0 {
		return &Result{Matches: nil, Truncated: candRes != nil && candRes.Truncated}, nil
	}

	docs, err := r.loader.LoadDocuments(ctx, req.Ontology, candRes.ObjectType, candRes.PrimaryKeys)
	if err != nil {
		return nil, fmt.Errorf("rag: load documents: %w", err)
	}

	// Drop documents with empty text — embedding zero-length strings
	// gives degenerate vectors and pollutes the ranking.
	filtered := docs[:0]
	for _, d := range docs {
		if strings.TrimSpace(d.Text) == "" {
			continue
		}
		filtered = append(filtered, d)
	}
	docs = filtered

	if len(docs) == 0 {
		return &Result{Matches: nil, Truncated: candRes.Truncated}, nil
	}

	// Embed the query alongside every document text in one batch so
	// providers that amortise per-call setup (auth, network) stay cheap.
	texts := make([]string, 0, len(docs)+1)
	texts = append(texts, req.Query)
	for _, d := range docs {
		texts = append(texts, d.Text)
	}
	vecs, err := r.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("rag: embed: %w", err)
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("rag: embed: provider returned %d vectors, want %d", len(vecs), len(texts))
	}

	queryVec := vecs[0]
	matches := make([]Match, 0, len(docs))
	for i, d := range docs {
		matches = append(matches, Match{
			Document: d,
			Score:    cosineSimilarity(queryVec, vecs[i+1]),
		})
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})
	if k < len(matches) {
		matches = matches[:k]
	}
	return &Result{Matches: matches, Truncated: candRes.Truncated}, nil
}

// cosineSimilarity returns the cosine of the angle between a and b.
// Vectors of mismatched length or zero magnitude return 0 — both are
// treated as "no signal" rather than an error so a single bad row in a
// large batch does not poison the entire retrieval.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		x := float64(a[i])
		y := float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
