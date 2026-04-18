package oms

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// LinkEdge represents a single row in the link_edges shared junction table.
// It is the on-the-wire representation of a many-to-many link between two
// objects, keyed by (link_type_rid, source_object_rid, target_object_rid).
//
// The string pk fields hold *object primary keys* (e.g. "10248"), not full
// RIDs — matching the convention used by the FK resolver and Bleve indexes.
// See migrations/000006_link_edges.up.sql for the physical schema.
type LinkEdge struct {
	LinkTypeRID    string          `json:"linkTypeRid"`
	SourceObjectPK string          `json:"sourceObjectPk"`
	TargetObjectPK string          `json:"targetObjectPk"`
	EdgeProperties json.RawMessage `json:"edgeProperties,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

// LinkEdgeStore is the narrow read/write surface over link_edges used by the
// US-210 edge-value handlers and the searchAround enrichment path. Kept out
// of oms.Repository for the same reason other narrow stores are — avoids
// stub churn in the ~15 mock repos across the tree.
type LinkEdgeStore interface {
	GetLinkEdge(ctx context.Context, linkTypeRID, sourcePK, targetPK string) (*LinkEdge, error)
	UpsertLinkEdge(ctx context.Context, edge *LinkEdge) error
	DeleteLinkEdge(ctx context.Context, linkTypeRID, sourcePK, targetPK string) error
	ListLinkEdgesWithProperties(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]LinkEdge, error)
	ListLinkEdgesWithPropertiesByTarget(ctx context.Context, linkTypeRID string, targetPKs []string) ([]LinkEdge, error)
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

// UpsertLinkEdge inserts or updates a single M2M link edge keyed by
// (link_type_rid, source_object_rid, target_object_rid). Edge properties are
// overwritten on conflict so callers can update them without first deleting.
func (r *PGRepository) UpsertLinkEdge(ctx context.Context, edge *LinkEdge) error {
	props := edge.EdgeProperties
	if len(props) == 0 {
		props = nil
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO link_edges (link_type_rid, source_object_rid, target_object_rid, edge_properties)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (link_type_rid, source_object_rid, target_object_rid)
		 DO UPDATE SET edge_properties = EXCLUDED.edge_properties`,
		edge.LinkTypeRID, edge.SourceObjectPK, edge.TargetObjectPK, props)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// DeleteLinkEdge removes a single M2M link edge. It is idempotent: deleting a
// nonexistent edge returns nil rather than ErrNotFound, so callers can safely
// retry without special-casing missing rows.
func (r *PGRepository) DeleteLinkEdge(ctx context.Context, linkTypeRID, sourcePK, targetPK string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM link_edges
		  WHERE link_type_rid = $1
		    AND source_object_rid = $2
		    AND target_object_rid = $3`,
		linkTypeRID, sourcePK, targetPK)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// GetLinkEdge fetches a single M2M edge including its edge_properties.
// Returns ErrNotFound when no row matches the (linkTypeRID, sourcePK,
// targetPK) triple. Added for US-210 so edge-property CRUD and the
// searchAround enrichment path have a precise lookup that surfaces
// edge_properties (ListEdgeTargets / ListEdgeSources drop them by design).
func (r *PGRepository) GetLinkEdge(ctx context.Context, linkTypeRID, sourcePK, targetPK string) (*LinkEdge, error) {
	edge := &LinkEdge{}
	var props []byte
	err := r.pool.QueryRow(ctx,
		`SELECT link_type_rid, source_object_rid, target_object_rid, edge_properties, created_at
		   FROM link_edges
		  WHERE link_type_rid = $1
		    AND source_object_rid = $2
		    AND target_object_rid = $3`,
		linkTypeRID, sourcePK, targetPK).
		Scan(&edge.LinkTypeRID, &edge.SourceObjectPK, &edge.TargetObjectPK, &props, &edge.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(props) > 0 {
		edge.EdgeProperties = props
	}
	return edge, nil
}

// ListLinkEdgesWithProperties returns the full edges (including
// edge_properties JSONB) for the given link type where source ∈ sourcePKs.
// Used by the searchAround enrichment path (US-210) so callers can surface
// per-edge property values alongside the traversal targets.
func (r *PGRepository) ListLinkEdgesWithProperties(ctx context.Context, linkTypeRID string, sourcePKs []string) ([]LinkEdge, error) {
	return r.listLinkEdgesWithProps(ctx, linkTypeRID, sourcePKs, true)
}

// ListLinkEdgesWithPropertiesByTarget is the reverse-direction counterpart to
// ListLinkEdgesWithProperties: matches edges where target ∈ targetPKs. The
// returned slice is ordered by (target, source) so reverse-enrichment tests
// observe a stable shape.
func (r *PGRepository) ListLinkEdgesWithPropertiesByTarget(ctx context.Context, linkTypeRID string, targetPKs []string) ([]LinkEdge, error) {
	return r.listLinkEdgesWithProps(ctx, linkTypeRID, targetPKs, false)
}

func (r *PGRepository) listLinkEdgesWithProps(ctx context.Context, linkTypeRID string, pks []string, byForward bool) ([]LinkEdge, error) {
	if len(pks) == 0 {
		return nil, nil
	}
	var sql string
	if byForward {
		sql = `SELECT link_type_rid, source_object_rid, target_object_rid, edge_properties, created_at
		         FROM link_edges
		        WHERE link_type_rid = $1
		          AND source_object_rid = ANY($2)
		        ORDER BY source_object_rid, target_object_rid`
	} else {
		sql = `SELECT link_type_rid, source_object_rid, target_object_rid, edge_properties, created_at
		         FROM link_edges
		        WHERE link_type_rid = $1
		          AND target_object_rid = ANY($2)
		        ORDER BY target_object_rid, source_object_rid`
	}
	rows, err := r.pool.Query(ctx, sql, linkTypeRID, pks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LinkEdge
	for rows.Next() {
		var edge LinkEdge
		var props []byte
		if err := rows.Scan(&edge.LinkTypeRID, &edge.SourceObjectPK, &edge.TargetObjectPK, &props, &edge.CreatedAt); err != nil {
			return nil, err
		}
		if len(props) > 0 {
			edge.EdgeProperties = props
		}
		out = append(out, edge)
	}
	return out, rows.Err()
}

// DeleteAllLinkEdgesForSource removes every M2M edge of the given link type
// whose source is sourcePK. Used when an object is being unlinked or deleted.
// Idempotent: returns nil even if no rows match.
func (r *PGRepository) DeleteAllLinkEdgesForSource(ctx context.Context, linkTypeRID, sourcePK string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM link_edges
		  WHERE link_type_rid = $1
		    AND source_object_rid = $2`,
		linkTypeRID, sourcePK)
	if err != nil {
		return wrapPGError(err)
	}
	return nil
}

// GetLinkTypeByAPIName looks up a LinkType by its API name within an ontology.
// The ontology argument can be either an ontology RID or an api_name; the same
// disjunctive lookup pattern used by ListLinkTypes applies here.
func (r *PGRepository) GetLinkTypeByAPIName(ctx context.Context, ontologyRID, apiName string) (*LinkType, error) {
	lt := &LinkType{}
	err := r.pool.QueryRow(ctx,
		`SELECT rid, ontology_rid, api_name, display_name, COALESCE(description, ''),
		 source_object_type, target_object_type, cardinality,
		 foreign_key_config, join_table_config, is_required,
		 COALESCE(inverse_link_rid, ''), propagate_markings, created_at
		 FROM link_types
		 WHERE (rid = $2 OR api_name = $2)
		   AND (ontology_rid = $1
		        OR ontology_rid = (SELECT rid FROM ontologies WHERE api_name = $1 LIMIT 1))`,
		ontologyRID, apiName).
		Scan(&lt.RID, &lt.OntologyRID, &lt.APIName, &lt.DisplayName, &lt.Description,
			&lt.SourceObjectType, &lt.TargetObjectType, &lt.Cardinality,
			&lt.ForeignKeyConfig, &lt.JoinTableConfig, &lt.IsRequired,
			&lt.InverseLinkRID, &lt.PropagateMarkings, &lt.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return lt, nil
}
