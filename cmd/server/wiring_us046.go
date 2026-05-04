package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"golang.org/x/time/rate"

	"github.com/liyang/weave/pkg/embeddings"
	"github.com/liyang/weave/pkg/funnel"
	"github.com/liyang/weave/pkg/mcp"
	"github.com/liyang/weave/pkg/oms"
	"github.com/liyang/weave/pkg/oss/objectset"
)

// pgVectorStore adapts oms.Repository.FindNearestNeighbors to the
// objectset.NNVectorStore interface used by the ObjectSet executor. It
// resolves the ObjectType API name to a RID via the OMS repository before
// running the kNN query, then post-filters by the executor's candidate set
// (the inner ObjectSet result) so the response respects ObjectSet
// composition.
//
// Lives under cmd/server/ rather than pkg/oss/ to avoid an import cycle:
// pkg/oss/objectset already depends on pkg/oss for the WireObject type, so
// pkg/oss cannot itself import pkg/oss/objectset. The adapter only needs to
// be visible at wiring time, so cmd/server is the right home.
type pgVectorStore struct {
	repo oms.Repository
}

func newPGVectorStore(repo oms.Repository) objectset.NNVectorStore {
	return &pgVectorStore{repo: repo}
}

func (s *pgVectorStore) FindNearestNeighbors(ctx context.Context, q objectset.NNVectorQuery) ([]objectset.NearestNeighborMatch, error) {
	if s.repo == nil {
		return nil, fmt.Errorf("pgVectorStore: repository not configured")
	}
	if q.ObjectType == "" {
		return nil, fmt.Errorf("pgVectorStore: object type required")
	}
	if len(q.QueryVector) == 0 {
		return nil, fmt.Errorf("pgVectorStore: empty query vector")
	}
	ot, err := s.repo.GetObjectTypeByAPIName(ctx, q.Ontology, q.ObjectType)
	if err != nil {
		return nil, fmt.Errorf("resolve object type %q: %w", q.ObjectType, err)
	}

	// Fetch a window slightly larger than k so post-filtering against the
	// candidate set still has enough rows when the candidates are a strict
	// subset of the corpus.
	window := q.K
	if len(q.CandidatePKs) > 0 {
		window = q.K * 5
		if window < 50 {
			window = 50
		}
		if window > 1000 {
			window = 1000
		}
	}
	if window <= 0 {
		window = 10
	}

	rows, err := s.repo.FindNearestNeighbors(ctx, ot.RID, q.QueryVector, window, q.Model)
	if err != nil {
		return nil, fmt.Errorf("pgvector knn: %w", err)
	}

	matches := make([]objectset.NearestNeighborMatch, 0, len(rows))
	if len(q.CandidatePKs) == 0 {
		for _, r := range rows {
			matches = append(matches, objectset.NearestNeighborMatch{
				PrimaryKey: r.PrimaryKey,
				Distance:   r.Distance,
			})
			if len(matches) >= q.K {
				break
			}
		}
		return matches, nil
	}

	allowed := make(map[string]struct{}, len(q.CandidatePKs))
	for _, pk := range q.CandidatePKs {
		allowed[pk] = struct{}{}
	}
	for _, r := range rows {
		if _, ok := allowed[r.PrimaryKey]; !ok {
			continue
		}
		matches = append(matches, objectset.NearestNeighborMatch{
			PrimaryKey: r.PrimaryKey,
			Distance:   r.Distance,
		})
		if len(matches) >= q.K {
			break
		}
	}
	return matches, nil
}

// US-046 wiring lives here so main.go stays focused on top-level boot order.
// Every helper is opt-in via env vars: when nothing is set, the embedding
// hook and semantic-search backend are dormant and existing behaviour is
// preserved.

// buildEmbeddingProvider returns the configured embeddings.Provider, or nil
// when no provider is enabled.
//
// Explicit selection (US-436): WEAVE_EMBED_PROVIDER picks one of
// `mock` / `openai` / `ollama` / `sentence_transformers` (case-insensitive,
// `-` and `_` interchangeable). When set, the matching backend is wired
// using its provider-specific env vars and the legacy fallbacks are skipped.
//
// Legacy fallback (preserved so existing setups keep working):
//  1. WEAVE_EMBED_MOCK=1 → MockProvider (deterministic, offline-friendly)
//  2. WEAVE_OPENAI_API_KEY set → OpenAI provider (text-embedding-3-small)
//  3. otherwise → nil (disabled)
//
// The mock takes precedence so a developer who explicitly opts into the
// deterministic backend isn't surprised by an OPENAI_API_KEY in their shell.
func buildEmbeddingProvider() embeddings.Provider {
	if explicit := strings.TrimSpace(os.Getenv("WEAVE_EMBED_PROVIDER")); explicit != "" {
		return buildEmbeddingProviderFor(strings.ToLower(strings.ReplaceAll(explicit, "-", "_")))
	}
	if v := os.Getenv("WEAVE_EMBED_MOCK"); v == "1" || strings.EqualFold(v, "true") {
		log.Printf("[EMBED] mock provider enabled (deterministic)")
		return embeddings.NewMockProvider()
	}
	if key := os.Getenv("WEAVE_OPENAI_API_KEY"); key != "" {
		log.Printf("[EMBED] OpenAI embeddings provider enabled (text-embedding-3-small)")
		return embeddings.NewOpenAIProvider(embeddings.OpenAIConfig{APIKey: key})
	}
	return nil
}

// buildEmbeddingProviderFor materialises one of the named providers. When
// a provider's required configuration is missing (e.g. no API key for
// OpenAI, no shim URL for sentence-transformers), the function logs a
// warning and returns nil so the server boots in disabled mode rather than
// crashing — matching the legacy fallback semantics.
func buildEmbeddingProviderFor(name string) embeddings.Provider {
	switch name {
	case "mock":
		log.Printf("[EMBED] mock provider enabled (WEAVE_EMBED_PROVIDER=mock)")
		return embeddings.NewMockProvider()
	case "openai":
		key := os.Getenv("WEAVE_OPENAI_API_KEY")
		if key == "" {
			key = os.Getenv("OPENAI_API_KEY")
		}
		if key == "" {
			log.Printf("[EMBED] WEAVE_EMBED_PROVIDER=openai but no API key set; provider disabled")
			return nil
		}
		cfg := embeddings.OpenAIConfig{APIKey: key}
		if base := os.Getenv("WEAVE_EMBED_OPENAI_BASE_URL"); base != "" {
			cfg.BaseURL = base
		}
		if model := os.Getenv("WEAVE_EMBED_MODEL"); model != "" {
			cfg.Model = model
		}
		log.Printf("[EMBED] OpenAI provider enabled (model=%s)", embeddingModelOrDefault(cfg.Model, "text-embedding-3-small"))
		return embeddings.NewOpenAIProvider(cfg)
	case "ollama":
		cfg := embeddings.OllamaConfig{}
		if base := os.Getenv("WEAVE_EMBED_OLLAMA_BASE_URL"); base != "" {
			cfg.BaseURL = base
		}
		if model := os.Getenv("WEAVE_EMBED_MODEL"); model != "" {
			cfg.Model = model
		}
		if dims := parsePositiveInt(os.Getenv("WEAVE_EMBED_DIMENSIONS")); dims > 0 {
			cfg.Dimensions = dims
		}
		log.Printf("[EMBED] Ollama provider enabled (base=%s, model=%s)",
			embeddingModelOrDefault(cfg.BaseURL, "http://localhost:11434"),
			embeddingModelOrDefault(cfg.Model, "nomic-embed-text"))
		return embeddings.NewOllamaProvider(cfg)
	case "sentence_transformers", "sentencetransformers", "st":
		base := os.Getenv("WEAVE_EMBED_ST_BASE_URL")
		if base == "" {
			log.Printf("[EMBED] WEAVE_EMBED_PROVIDER=sentence_transformers but WEAVE_EMBED_ST_BASE_URL not set; provider disabled")
			return nil
		}
		cfg := embeddings.SentenceTransformersConfig{BaseURL: base}
		if model := os.Getenv("WEAVE_EMBED_MODEL"); model != "" {
			cfg.Model = model
		}
		if dims := parsePositiveInt(os.Getenv("WEAVE_EMBED_DIMENSIONS")); dims > 0 {
			cfg.Dimensions = dims
		}
		if key := os.Getenv("WEAVE_EMBED_ST_API_KEY"); key != "" {
			cfg.APIKey = key
		}
		log.Printf("[EMBED] sentence-transformers provider enabled (base=%s, model=%s)",
			cfg.BaseURL, embeddingModelOrDefault(cfg.Model, "sentence-transformers/all-MiniLM-L6-v2"))
		return embeddings.NewSentenceTransformersProvider(cfg)
	default:
		log.Printf("[EMBED] unknown WEAVE_EMBED_PROVIDER=%q; provider disabled", name)
		return nil
	}
}

func embeddingModelOrDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func parsePositiveInt(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// buildEmbeddingRateLimiter returns the rate limiter that gates funnel
// embedding generation. WEAVE_EMBED_RATE_PER_SEC is parsed as a float (e.g.
// "0.5" means one embedding every two seconds). Defaults to 5/sec with a
// burst of 5 when unset.
func buildEmbeddingRateLimiter() *rate.Limiter {
	rateStr := os.Getenv("WEAVE_EMBED_RATE_PER_SEC")
	r := 5.0
	if rateStr != "" {
		if parsed, err := strconv.ParseFloat(rateStr, 64); err == nil && parsed > 0 {
			r = parsed
		}
	}
	burst := int(r)
	if burst < 1 {
		burst = 1
	}
	return rate.NewLimiter(rate.Limit(r), burst)
}

// loadEmbeddingObjectTypes parses WEAVE_EMBED_OBJECT_TYPES — a
// comma-separated list of `objectType:prop1+prop2+...` entries — into the
// per-type embedding configuration consumed by the funnel hook.
//
// Example:  WEAVE_EMBED_OBJECT_TYPES=Document:title+body,User:name+bio
//
// Returns an empty map (NOT nil) when the env var is unset; the funnel
// consumer treats both as "no types configured" and skips embed generation.
func loadEmbeddingObjectTypes() map[string]funnel.EmbeddingConfig {
	out := map[string]funnel.EmbeddingConfig{}
	v := os.Getenv("WEAVE_EMBED_OBJECT_TYPES")
	if v == "" {
		return out
	}
	for _, entry := range strings.Split(v, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			continue
		}
		objType := strings.TrimSpace(parts[0])
		props := strings.Split(parts[1], "+")
		clean := make([]string, 0, len(props))
		for _, p := range props {
			if t := strings.TrimSpace(p); t != "" {
				clean = append(clean, t)
			}
		}
		if objType != "" && len(clean) > 0 {
			out[objType] = funnel.EmbeddingConfig{SourceProperties: clean}
		}
	}
	return out
}

// loadObjectTypeRIDs returns the API-name -> RID lookup table the funnel
// consumer needs to scope embedding rows. Walks every ontology and every
// object type via the OMS repo. Failures are logged but never fatal — the
// funnel hook falls back to using the API name as the RID.
func loadObjectTypeRIDs(ctx context.Context, repo oms.Repository) map[string]string {
	out := map[string]string{}
	if repo == nil {
		return out
	}
	onts, err := repo.ListOntologies(ctx)
	if err != nil {
		log.Printf("[EMBED] list ontologies: %v", err)
		return out
	}
	for _, o := range onts {
		ots, err := repo.ListObjectTypes(ctx, o.RID)
		if err != nil {
			log.Printf("[EMBED] list objectTypes for %s: %v", o.APIName, err)
			continue
		}
		for _, ot := range ots {
			out[ot.APIName] = ot.RID
		}
	}
	return out
}

// executorSemanticSearcher adapts an objectset.Executor to the
// mcp.SemanticSearcher interface. It builds a nearestNeighbors definition
// from the request and runs it through the executor, which delegates to the
// configured vector store and embedding provider.
type executorSemanticSearcher struct {
	exec *objectset.Executor
}

func newExecutorSemanticSearcher(exec *objectset.Executor) mcp.SemanticSearcher {
	return &executorSemanticSearcher{exec: exec}
}

// SemanticSearch builds an objectset.Definition that wraps a base set in a
// nearestNeighbors clause, executes it, and returns the result as a list of
// SemanticHit. Distances are not currently surfaced by Result; the
// executor returns ranked PKs.
func (s *executorSemanticSearcher) SemanticSearch(ctx context.Context, req mcp.SemanticSearchRequest) (*mcp.SemanticSearchResult, error) {
	k := req.K
	if k <= 0 {
		k = 10
	}
	def := &objectset.Definition{
		Type: "nearestNeighbors",
		ObjectSet: &objectset.Definition{
			Type:       "base",
			ObjectType: req.ObjectType,
		},
		PropertyIdentifier: &objectset.PropertyIdentifier{
			Property: struct {
				APIName string `json:"apiName"`
			}{APIName: "embedding"},
		},
		NumNeighbors: &k,
		Query:        &objectset.NNQuery{Text: &objectset.TextQuery{Value: req.QueryText}},
	}
	scoped := objectset.WithOntologyScope(ctx, req.Ontology)
	res, err := s.exec.Execute(scoped, def)
	if err != nil {
		return nil, err
	}
	hits := make([]mcp.SemanticHit, 0, len(res.PrimaryKeys))
	for _, pk := range res.PrimaryKeys {
		hits = append(hits, mcp.SemanticHit{PrimaryKey: pk})
	}
	return &mcp.SemanticSearchResult{Hits: hits}, nil
}
