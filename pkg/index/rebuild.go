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

// Rebuild drops the index for (ontology, objectType), recreates it with a
// fresh mapping derived from the current property schema, then replays every
// document returned by src into the new index. It is safe to run repeatedly
// and is idempotent: a second call on the same ObjectType produces the same
// final state.
//
// Errors from any step short-circuit: a failing drop, missing metadata, or a
// source that returns an error all propagate up so the caller can surface
// them to the CLI / HTTP client. After a successful Rebuild the index is
// open in the manager and ready to serve queries.
func Rebuild(ctx context.Context, mgr *Manager, repo RebuildRepo, src LatestDocumentSource, req RebuildRequest) (*RebuildResult, error) {
	if mgr == nil {
		return nil, errors.New("rebuild: nil index manager")
	}
	if repo == nil {
		return nil, errors.New("rebuild: nil metadata repo")
	}
	if req.OntologyAPIName == "" || req.ObjectTypeAPIName == "" {
		return nil, errors.New("rebuild: ontology and objectType are required")
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

	// Drop + recreate. DropIndex tolerates a missing index (returns nil), so
	// a rebuild on a cold machine works without pre-existing state.
	if err := mgr.DropIndex(scopedKey); err != nil {
		return nil, fmt.Errorf("rebuild: drop index %q: %w", scopedKey, err)
	}
	if _, err := mgr.EnsureIndex(scopedKey, indexProps); err != nil {
		return nil, fmt.Errorf("rebuild: recreate index %q: %w", scopedKey, err)
	}

	if src == nil {
		return &RebuildResult{ScopedKey: scopedKey, IndexedCount: 0}, nil
	}

	docs, err := src.LoadLatestDocuments(ctx, ot.RID)
	if err != nil {
		return nil, fmt.Errorf("rebuild: load documents: %w", err)
	}

	if len(docs) == 0 {
		return &RebuildResult{ScopedKey: scopedKey, IndexedCount: 0}, nil
	}

	ops := make([]BatchOp, 0, len(docs))
	for _, d := range docs {
		ops = append(ops, BatchOp{
			Type:       BatchOpIndex,
			PrimaryKey: d.PrimaryKey,
			Document:   d.Body,
		})
	}
	if err := mgr.ApplyBatch(scopedKey, ops); err != nil {
		return nil, fmt.Errorf("rebuild: commit batch: %w", err)
	}

	return &RebuildResult{ScopedKey: scopedKey, IndexedCount: len(docs)}, nil
}
