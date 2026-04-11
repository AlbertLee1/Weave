package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/liyang/weave/pkg/index"
	"github.com/liyang/weave/pkg/oms"
)

// pgIndexDocSource adapts *oms.PGRepository to index.LatestDocumentSource.
// Rebuilds read the per-primary-key tail of object_history via
// LoadLatestObjectStates and materialise it into the property-map shape
// Bleve consumes. DELETE tombstones are already filtered upstream so there
// is nothing more to do here besides unmarshal.
type pgIndexDocSource struct {
	repo *oms.PGRepository
}

// newPGIndexDocSource returns nil when repo is nil so main.go can call it
// unconditionally in degraded mode (no PG pool → no rebuild data source).
func newPGIndexDocSource(repo *oms.PGRepository) index.LatestDocumentSource {
	if repo == nil {
		return nil
	}
	return &pgIndexDocSource{repo: repo}
}

func (s *pgIndexDocSource) LoadLatestDocuments(ctx context.Context, objectTypeRID string) ([]index.LatestDocument, error) {
	rows, err := s.repo.LoadLatestObjectStates(ctx, objectTypeRID)
	if err != nil {
		return nil, err
	}
	docs := make([]index.LatestDocument, 0, len(rows))
	for _, r := range rows {
		var body map[string]interface{}
		if err := json.Unmarshal(r.NewState, &body); err != nil {
			return nil, fmt.Errorf("decode new_state for %q: %w", r.PrimaryKey, err)
		}
		docs = append(docs, index.LatestDocument{
			PrimaryKey: r.PrimaryKey,
			Body:       body,
		})
	}
	return docs, nil
}
