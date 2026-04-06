package oms

import (
	"context"
	"encoding/json"
	"time"
)

// LinkEdge represents a single row in the link_edges shared junction table.
// It is the on-the-wire representation of a many-to-many link between two
// objects, keyed by (link_type_rid, source_object_rid, target_object_rid).
//
// The string pk fields hold *object primary keys* (e.g. "10248"), not full
// RIDs — matching the convention used by the FK resolver and Bleve indexes.
// See migrations/000006_link_edges.up.sql for the physical schema.
type LinkEdge struct {
	LinkTypeRID     string          `json:"linkTypeRid"`
	SourceObjectPK  string          `json:"sourceObjectPk"`
	TargetObjectPK  string          `json:"targetObjectPk"`
	EdgeProperties  json.RawMessage `json:"edgeProperties,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
}

// ListEdgeTargets returns distinct target PKs for edges of the given link
// type whose source PK is in sourcePKs. Implements links.EdgeRepository.
func (r *PGRepository) ListEdgeTargets(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]string, error) {
	if len(sourcePKs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT target_object_rid
		   FROM link_edges
		  WHERE link_type_rid = $1
		    AND source_object_rid = ANY($2)
		  ORDER BY target_object_rid`,
		linkTypeRID, sourcePKs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, err
		}
		result = append(result, pk)
	}
	return result, rows.Err()
}

// ListEdgeSources returns distinct source PKs for edges of the given link
// type whose target PK is in targetPKs. Implements links.EdgeRepository for
// reverse traversal.
func (r *PGRepository) ListEdgeSources(ctx context.Context, linkTypeRID string, targetPKs []string) ([]string, error) {
	if len(targetPKs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT DISTINCT source_object_rid
		   FROM link_edges
		  WHERE link_type_rid = $1
		    AND target_object_rid = ANY($2)
		  ORDER BY source_object_rid`,
		linkTypeRID, targetPKs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var pk string
		if err := rows.Scan(&pk); err != nil {
			return nil, err
		}
		result = append(result, pk)
	}
	return result, rows.Err()
}
