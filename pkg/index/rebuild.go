package index

import (
	"context"
	"errors"
	"fmt"

	"github.com/liyang/weave/pkg/oms"
)

// RebuildRepo is the narrow slice of oms.Repository that Rebuild needs to
// resolve (ontologyAPIName, objectTypeAPIName) to a scoped Bleve key and a
// property list suitable for BuildMapping. Both *oms.CachedRepository and
// *oms.PGRepository satisfy it.
type RebuildRepo interface {
	GetOntology(ctx context.Context, ridOrAPIName string) (*oms.Ontology, error)
	GetObjectTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*oms.ObjectType, error)
	ListProperties(ctx context.Context, objectTypeRID string) ([]oms.Property, error)
}

// LatestDocument is the post-edit snapshot of a single primary key for a
// single ObjectType, in the same shape the funnel consumer would push via
// IndexDocument. Body is the property-map Bleve consumes directly.
type LatestDocument struct {
	PrimaryKey string
	Body       map[string]interface{}
}

// LatestDocumentSource loads the current state of every primary key for a
// single ObjectType. Backends may read from Parquet snapshots (fast path),
// PG object_history (authoritative fallback), or any other durable store.
//
// Nil implementations are allowed; Rebuild treats a nil source as "rebuild
// the index shell but leave it empty" — useful when the operator has
// deliberately wiped WEAVE_DATA_DIR and just wants queries to stop failing.
type LatestDocumentSource interface {
	LoadLatestDocuments(ctx context.Context, objectTypeRID string) ([]LatestDocument, error)
}

// RebuildRequest identifies the target of an index rebuild by API names.
type RebuildRequest struct {
	OntologyAPIName   string
	ObjectTypeAPIName string
}

// RebuildResult is the successful outcome of a rebuild: the fully-scoped
// Bleve key that was rebuilt and the number of documents that were indexed.
// A zero IndexedCount is not an error — it simply means the source had no
// rows for the ObjectType (e.g. a brand-new type).
type RebuildResult struct {
	ScopedKey    string
	IndexedCount int
}

// RebuildStage labels each phase of the index rebuild lifecycle so progress
// callbacks can render meaningful UI without parsing free-form strings.
type RebuildStage string

const (
	// RebuildStageStart fires once at the very top, before metadata is loaded.
	RebuildStageStart RebuildStage = "start"
	// RebuildStageDrop fires after the existing index has been dropped.
	RebuildStageDrop RebuildStage = "drop"
	// RebuildStageRecreate fires after the empty index shell is recreated.
	RebuildStageRecreate RebuildStage = "recreate"
	// RebuildStageEstimate fires once the document count is known. Total
	// carries the estimate so the CLI can render a progress bar.
	RebuildStageEstimate RebuildStage = "estimate"
	// RebuildStageBatch fires once per batched commit while documents are
	// streaming into Bleve. Current is the running indexed count, Total is
	// the same as the estimate.
	RebuildStageBatch RebuildStage = "batch"
	// RebuildStageComplete fires once at the end on a successful rebuild.
	RebuildStageComplete RebuildStage = "complete"
)

// ProgressEvent is delivered to RebuildOptions.Progress at every lifecycle
// transition. Total is 0 until RebuildStageEstimate fires.
type ProgressEvent struct {
	Stage     RebuildStage
	ScopedKey string
	Current   int
	Total     int
}

// DefaultRebuildBatchSize is the chunk size used when RebuildOptions.BatchSize
// is unset or non-positive. Bleve commits stay below the 4 MiB write-ahead
// limit comfortably at 1k docs per batch on the typical Northwind / Chinook
// fixture sizes.
const DefaultRebuildBatchSize = 1000

// RebuildOptions tunes the rebuild lifecycle. All fields are optional; a
// zero-value RebuildOptions is identical to calling the legacy Rebuild
// (single-shot batch commit, no progress callback).
type RebuildOptions struct {
	// Progress, when non-nil, is invoked at every RebuildStage transition.
	// The callback runs on the rebuild goroutine and MUST NOT block.
	Progress func(ProgressEvent)
	// BatchSize controls the chunk size of Bleve batch commits during the
	// indexing phase. Non-positive values fall back to DefaultRebuildBatchSize.
	BatchSize int
}

// Rebuild is the legacy single-shot rebuild entry point retained for
// backwards compatibility with callers that do not need progress reporting
// or batching. New callers should prefer RebuildWithOptions.
func Rebuild(ctx context.Context, mgr *Manager, repo RebuildRepo, src LatestDocumentSource, req RebuildRequest) (*RebuildResult, error) {
	return RebuildWithOptions(ctx, mgr, repo, src, req, RebuildOptions{})
}

// RebuildWithOptions drops the index for (ontology, objectType), recreates
// it with a fresh mapping derived from the current property schema, then
// replays every document returned by src into the new index in batches of
// opts.BatchSize. Progress callbacks fire at every lifecycle transition so
// CLI / UI consumers can render an estimate + progress bar.
//
// The Manager's rebuild marker is set for the entire critical section
// (drop → recreate → batch commits) so the OSS executor's hot-path
// IsRebuilding probe can transparently route base reads through the cold
// tier (US-408). The marker is cleared via defer even on error paths.
//
// Errors from any step short-circuit: a failing drop, missing metadata, or
// a source that returns an error all propagate up so the caller can surface
// them to the CLI / HTTP client. After a successful return the index is
// open in the manager and ready to serve queries.
func RebuildWithOptions(ctx context.Context, mgr *Manager, repo RebuildRepo, src LatestDocumentSource, req RebuildRequest, opts RebuildOptions) (*RebuildResult, error) {
	if mgr == nil {
		return nil, errors.New("rebuild: nil index manager")
	}
	if repo == nil {
		return nil, errors.New("rebuild: nil metadata repo")
	}
	if req.OntologyAPIName == "" || req.ObjectTypeAPIName == "" {
		return nil, errors.New("rebuild: ontology and objectType are required")
	}

	emit := func(ev ProgressEvent) {
		if opts.Progress != nil {
			opts.Progress(ev)
		}
	}

	ont, err := repo.GetOntology(ctx, req.OntologyAPIName)
	if err != nil {
		return nil, fmt.Errorf("rebuild: lookup ontology %q: %w", req.OntologyAPIName, err)
	}
	ot, err := repo.GetObjectTypeByAPIName(ctx, ont.RID, req.ObjectTypeAPIName)
	if err != nil {
		return nil, fmt.Errorf("rebuild: lookup objectType %q: %w", req.ObjectTypeAPIName, err)
	}
	props, err := repo.ListProperties(ctx, ot.RID)
	if err != nil {
		return nil, fmt.Errorf("rebuild: list properties: %w", err)
	}

	indexProps := make([]Property, 0, len(props))
	for _, p := range props {
		indexProps = append(indexProps, Property{
			APIName:      p.APIName,
			BaseType:     p.BaseType,
			IsSearchable: p.IsSearchable,
			IsArray:      p.IsArray,
			Analyzer:     AnalyzerFromTypeConfig(p.TypeConfig),
		})
	}

	scopedKey := ScopedKey(ont.APIName, ot.APIName)
	emit(ProgressEvent{Stage: RebuildStageStart, ScopedKey: scopedKey})

	// Stamp the rebuild marker for the entire critical section. Defer the
	// clear so a panic in DropIndex / EnsureIndex / ApplyBatch still leaves
	// the executor's IsRebuilding probe in a consistent state.
	mgr.MarkRebuildStart(scopedKey)
	defer mgr.MarkRebuildEnd(scopedKey)

	// Drop + recreate. DropIndex tolerates a missing index (returns nil), so
	// a rebuild on a cold machine works without pre-existing state.
	if err := mgr.DropIndex(scopedKey); err != nil {
		return nil, fmt.Errorf("rebuild: drop index %q: %w", scopedKey, err)
	}
	emit(ProgressEvent{Stage: RebuildStageDrop, ScopedKey: scopedKey})

	if _, err := mgr.EnsureIndex(scopedKey, indexProps); err != nil {
		return nil, fmt.Errorf("rebuild: recreate index %q: %w", scopedKey, err)
	}
	emit(ProgressEvent{Stage: RebuildStageRecreate, ScopedKey: scopedKey})

	if src == nil {
		emit(ProgressEvent{Stage: RebuildStageEstimate, ScopedKey: scopedKey, Total: 0})
		emit(ProgressEvent{Stage: RebuildStageComplete, ScopedKey: scopedKey})
		return &RebuildResult{ScopedKey: scopedKey, IndexedCount: 0}, nil
	}

	docs, err := src.LoadLatestDocuments(ctx, ot.RID)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load documents: %w", err)
	}
	emit(ProgressEvent{Stage: RebuildStageEstimate, ScopedKey: scopedKey, Total: len(docs)})

	if len(docs) == 0 {
		emit(ProgressEvent{Stage: RebuildStageComplete, ScopedKey: scopedKey})
		return &RebuildResult{ScopedKey: scopedKey, IndexedCount: 0}, nil
	}

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultRebuildBatchSize
	}

	indexed := 0
	for start := 0; start < len(docs); start += batchSize {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("rebuild: %w", err)
		}
		end := start + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		ops := make([]BatchOp, 0, end-start)
		for _, d := range docs[start:end] {
			ops = append(ops, BatchOp{
				Type:       BatchOpIndex,
				PrimaryKey: d.PrimaryKey,
				Document:   d.Body,
			})
		}
		if err := mgr.ApplyBatch(scopedKey, ops); err != nil {
			return nil, fmt.Errorf("rebuild: commit batch: %w", err)
		}
		indexed = end
		emit(ProgressEvent{Stage: RebuildStageBatch, ScopedKey: scopedKey, Current: indexed, Total: len(docs)})
	}

	emit(ProgressEvent{Stage: RebuildStageComplete, ScopedKey: scopedKey, Current: indexed, Total: len(docs)})
	return &RebuildResult{ScopedKey: scopedKey, IndexedCount: indexed}, nil
}
