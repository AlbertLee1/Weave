package funnel

import (
	"context"
	"log"
)

// EditMaterializer is the optional consumer hook that persists every
// applied EditBatch to durable columnar storage (US-405). The production
// implementation is *materialize.Materializer (Parquet on local disk);
// tests inject a fake to assert the consumer dispatches correctly.
//
// Implementations are expected to be safe for concurrent use and must
// never mutate the supplied EditBatch — the consumer fans the same value
// out to multiple optional hooks (history, embeddings, materialize) and
// each one must see the post-conflict-resolution snapshot intact.
type EditMaterializer interface {
	MaterializeBatch(ctx context.Context, batch EditBatch) error
}

// SetEditMaterializer wires the optional materialization side channel.
// When set, every successfully applied batch is forwarded to the
// materializer AFTER the index commit. Failures are logged but never
// abort the batch — the index is the source of truth for read paths
// and materialization is best-effort durable storage. Pass nil to
// disable. Safe to call before Start().
func (c *Consumer) SetEditMaterializer(m EditMaterializer) {
	c.materializer = m
}

// runMaterialize fans a successfully applied batch out to the configured
// materializer. Errors are logged but never propagated so the consumer's
// happy path stays unchanged when the disk fills up or the writer
// goroutine errors on encoding.
func (c *Consumer) runMaterialize(ctx context.Context, batch EditBatch) {
	if c.materializer == nil {
		return
	}
	if err := c.materializer.MaterializeBatch(ctx, batch); err != nil {
		log.Printf("funnel: materialize error for ontology=%s batchID=%s: %v",
			batch.OntologyAPIName, batch.ID, err)
	}
}
