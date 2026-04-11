package index

import (
	"context"
	"fmt"

	"github.com/liyang/weave/pkg/oms"
)

// rehydrateRepo is the slice of oms.Repository that EnsureAllIndexes needs.
// Defining it locally lets the function accept any subset that satisfies it,
// and keeps the test stub small. The full oms.Repository implements it.
type rehydrateRepo interface {
	ListOntologies(ctx context.Context) ([]oms.Ontology, error)
	ListObjectTypes(ctx context.Context, ontologyRID string) ([]oms.ObjectType, error)
	ListProperties(ctx context.Context, objectTypeRID string) ([]oms.Property, error)
}

// EnsureAllIndexes walks every ObjectType in the repo and ensures a Bleve
// index shell exists for it with the correct property mappings. It is safe
// to run on every cold start: existing indexes are reused via EnsureIndex's
// idempotent open-or-create semantics.
//
// Note: This creates index shells with mappings but does NOT re-ingest
// historical object data. NATS JetStream uses WorkQueuePolicy retention,
// which deletes edits after ack, so a full rebuild from the event stream is
// not possible. For disaster recovery, operators should back up
// WEAVE_DATA_DIR separately. The purpose of this function is to prevent
// "index not found" errors on queries that hit an ObjectType which exists
// in PG metadata but has not yet received any writes from the funnel.
//
// Nil mgr or nil repo are no-ops so callers can run this unconditionally
// from the bootstrap path even when one or both dependencies are missing
// (degraded mode).
func EnsureAllIndexes(ctx context.Context, mgr *Manager, repo rehydrateRepo) error {
	if mgr == nil || repo == nil {
		return nil
	}

	ontologies, err := repo.ListOntologies(ctx)
	if err != nil {
		return fmt.Errorf("rehydrate: list ontologies: %w", err)
	}

	for _, ont := range ontologies {
		objectTypes, err := repo.ListObjectTypes(ctx, ont.RID)
		if err != nil {
			return fmt.Errorf("rehydrate: list object types for %q: %w", ont.RID, err)
		}

		for _, ot := range objectTypes {
			props, err := repo.ListProperties(ctx, ot.RID)
			if err != nil {
				return fmt.Errorf("rehydrate: list properties for %q: %w", ot.RID, err)
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
			if _, err := mgr.EnsureIndex(scopedKey, indexProps); err != nil {
				return fmt.Errorf("rehydrate: ensure index for %q: %w", scopedKey, err)
			}
		}
	}

	return nil
}
