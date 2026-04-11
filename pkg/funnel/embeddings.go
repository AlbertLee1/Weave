package funnel

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"golang.org/x/time/rate"

	"github.com/liyang/weave/pkg/oms"
)

// EmbeddingProvider is the funnel consumer's view of an embedding model.
// Implementations are expected to be safe for concurrent use.
type EmbeddingProvider interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Model() string
}

// EmbeddingStore is the persistent backend for object embeddings. The
// production implementation is oms.Repository (PG + pgvector); tests inject
// a fake. Defined as an interface so the funnel package never imports the
// full oms.Repository surface for one method.
type EmbeddingStore interface {
	UpsertObjectEmbedding(ctx context.Context, e *oms.ObjectEmbedding) error
}

// EmbeddingConfig describes how the consumer should derive the source text
// for an object's embedding. SourceProperties lists the property API names
// to concatenate (in order) into the embedding source text. An empty list
// disables embedding generation for the object type even if it's mapped.
type EmbeddingConfig struct {
	SourceProperties []string
}

// SetEmbeddingProvider wires an EmbeddingProvider so the consumer generates
// vectors on CREATE/MODIFY edits for configured object types. Pass nil to
// disable. Safe to call before Start().
func (c *Consumer) SetEmbeddingProvider(p EmbeddingProvider) {
	c.embedMu.Lock()
	defer c.embedMu.Unlock()
	c.embedProvider = p
}

// SetEmbeddingStore wires the persistent embedding store. Pass nil to
// disable. Safe to call before Start().
func (c *Consumer) SetEmbeddingStore(s EmbeddingStore) {
	c.embedMu.Lock()
	defer c.embedMu.Unlock()
	c.embedStore = s
}

// SetEmbeddingObjectTypes installs the per-object-type configuration that
// drives source-text extraction. The map is copied; subsequent caller
// mutations have no effect. Object types absent from the map are skipped.
func (c *Consumer) SetEmbeddingObjectTypes(m map[string]EmbeddingConfig) {
	c.embedMu.Lock()
	defer c.embedMu.Unlock()
	cp := make(map[string]EmbeddingConfig, len(m))
	for k, v := range m {
		props := append([]string(nil), v.SourceProperties...)
		cp[k] = EmbeddingConfig{SourceProperties: props}
	}
	c.embedTypes = cp
}

// SetEmbeddingObjectTypeRIDs supplies the API-name -> RID mapping the
// embedding store needs to scope rows. The map is copied. When an entry is
// missing the consumer falls back to the API name string as the RID.
func (c *Consumer) SetEmbeddingObjectTypeRIDs(m map[string]string) {
	c.embedMu.Lock()
	defer c.embedMu.Unlock()
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	c.embedRIDs = cp
}

// SetEmbeddingRateLimiter installs a token-bucket rate limiter that gates
// every embedding call. When AllowN returns false the edit is skipped (and
// logged) — back-pressure is preferable to blocking the index commit. Pass
// nil to disable rate limiting.
func (c *Consumer) SetEmbeddingRateLimiter(l *rate.Limiter) {
	c.embedMu.Lock()
	defer c.embedMu.Unlock()
	c.embedLimiter = l
}

// embedFields holds all consumer state for the embedding hook. Kept in a
// dedicated struct so the existing Consumer surface stays focused on
// indexing — embedding is an opt-in side channel.
type embedFields struct {
	embedMu       sync.RWMutex
	embedProvider EmbeddingProvider
	embedStore    EmbeddingStore
	embedTypes    map[string]EmbeddingConfig
	embedRIDs     map[string]string
	embedLimiter  *rate.Limiter
}

// snapshotEmbeddingState returns a consistent snapshot of the embedding hook
// state under the read lock so the caller can release the lock before doing
// any work.
func (c *Consumer) snapshotEmbeddingState() (EmbeddingProvider, EmbeddingStore, map[string]EmbeddingConfig, map[string]string, *rate.Limiter) {
	c.embedMu.RLock()
	defer c.embedMu.RUnlock()
	return c.embedProvider, c.embedStore, c.embedTypes, c.embedRIDs, c.embedLimiter
}

// generateEmbeddings runs the embedding side channel for one batch. Failures
// are logged but never returned, so the caller (applyBatchWithHistory) can
// commit the index even when the embedding backend is unavailable.
//
// Rate limiting: when a Limiter is configured the consumer calls AllowN(1)
// per edit. AllowN drops the edit on the floor when the bucket is empty
// rather than blocking the commit goroutine — operators set the limiter
// rate based on their model provider's per-second budget.
func (c *Consumer) generateEmbeddings(ctx context.Context, batch EditBatch) {
	provider, store, types, rids, limiter := c.snapshotEmbeddingState()
	if provider == nil || store == nil || len(types) == 0 {
		return
	}

	for _, edit := range batch.Edits {
		if edit.Type == EditTypeDelete {
			continue
		}
		cfg, ok := types[edit.ObjectType]
		if !ok || len(cfg.SourceProperties) == 0 {
			continue
		}
		text := buildEmbeddingText(edit.Properties, cfg.SourceProperties)
		if text == "" {
			continue
		}
		if limiter != nil && !limiter.Allow() {
			log.Printf("funnel: embedding throttled for %s/%s", edit.ObjectType, edit.PrimaryKey)
			continue
		}
		vecs, err := provider.Embed(ctx, []string{text})
		if err != nil {
			log.Printf("funnel: embed error for %s/%s: %v", edit.ObjectType, edit.PrimaryKey, err)
			continue
		}
		if len(vecs) != 1 || len(vecs[0]) == 0 {
			log.Printf("funnel: embed returned empty vector for %s/%s", edit.ObjectType, edit.PrimaryKey)
			continue
		}
		otRID := rids[edit.ObjectType]
		if otRID == "" {
			otRID = edit.ObjectType
		}
		row := &oms.ObjectEmbedding{
			ObjectTypeRID: otRID,
			PrimaryKey:    edit.PrimaryKey,
			Embedding:     vecs[0],
			Model:         provider.Model(),
			SourceText:    text,
		}
		if err := store.UpsertObjectEmbedding(ctx, row); err != nil {
			log.Printf("funnel: embedding upsert error for %s/%s: %v", edit.ObjectType, edit.PrimaryKey, err)
		}
	}
}

// buildEmbeddingText concatenates the configured source property values from
// an edit's properties map. Properties are read in the order declared by the
// caller so two identical objects always produce the same source text.
// Missing or non-stringable properties are skipped silently. Returns "" when
// nothing usable was found.
func buildEmbeddingText(props map[string]interface{}, sourceProps []string) string {
	if len(props) == 0 || len(sourceProps) == 0 {
		return ""
	}
	parts := make([]string, 0, len(sourceProps))
	for _, name := range sourceProps {
		v, ok := props[name]
		if !ok || v == nil {
			continue
		}
		s := stringifyProperty(v)
		if s == "" {
			continue
		}
		parts = append(parts, s)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// stringifyProperty turns an arbitrary JSON-decoded value into a single-line
// string suitable for embedding. Slices and maps are flattened to space-
// separated key/value pairs. We deliberately avoid json.Marshal because the
// embedding model gets better signal from natural language than from JSON
// punctuation.
func stringifyProperty(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, x := range t {
			s := stringifyProperty(x)
			if s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, " ")
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			s := stringifyProperty(t[k])
			if s != "" {
				parts = append(parts, k+" "+s)
			}
		}
		return strings.Join(parts, " ")
	default:
		return fmt.Sprintf("%v", t)
	}
}
