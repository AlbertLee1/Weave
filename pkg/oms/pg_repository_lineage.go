package oms

import (
	"context"
	"time"
)

// defaultLineageListLimit caps the per-query row count when callers omit
// (or pass a non-positive) limit so a runaway list call cannot drag the
// whole table back. 200 mirrors aip/logic.maxRunPageSize and oms.ListObjectHistory.
const defaultLineageListLimit = 200

// InsertLineageEdge appends one lineage edge and back-fills edge.ID +
// edge.Timestamp on success. A zero-valued edge.Timestamp falls through to
// the column DEFAULT NOW() so callers stay time-source agnostic; a non-zero
// caller value is written verbatim so deterministic-clock tests can pin the
// timestamp.
func (r *PGRepository) InsertLineageEdge(ctx context.Context, edge *LineageEdge) error {
	op := edge.Operation
	if !edge.Timestamp.IsZero() {
		return r.pool.QueryRow(ctx,
			`INSERT INTO lineage_edges (upstream_rid, downstream_rid, operation, ts)
			 VALUES ($1, $2, $3, $4)
			 RETURNING id, ts`,
			edge.UpstreamRID, edge.DownstreamRID, op, edge.Timestamp).
			Scan(&edge.ID, &edge.Timestamp)
	}
	return r.pool.QueryRow(ctx,
		`INSERT INTO lineage_edges (upstream_rid, downstream_rid, operation)
		 VALUES ($1, $2, $3)
		 RETURNING id, ts`,
		edge.UpstreamRID, edge.DownstreamRID, op).
		Scan(&edge.ID, &edge.Timestamp)
}

// ListUpstreamLineage returns the most recent up-to limit edges whose
// downstream_rid matches. Newest-first by ts (then id as a tiebreaker so
// rows that landed in the same TIMESTAMPTZ tick still emerge in a stable
// order).
func (r *PGRepository) ListUpstreamLineage(ctx context.Context, downstreamRID string, limit int) ([]LineageEdge, error) {
	if limit <= 0 {
		limit = defaultLineageListLimit
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, upstream_rid, downstream_rid, operation, ts
		 FROM lineage_edges
		 WHERE downstream_rid = $1
		 ORDER BY ts DESC, id DESC
		 LIMIT $2`,
		downstreamRID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLineageEdges(rows)
}

// ListDownstreamLineage is the inverse — given an upstream RID, return the
// most recent up-to limit downstream edges.
func (r *PGRepository) ListDownstreamLineage(ctx context.Context, upstreamRID string, limit int) ([]LineageEdge, error) {
	if limit <= 0 {
		limit = defaultLineageListLimit
	}
	rows, err := r.pool.Query(ctx,
		`SELECT id, upstream_rid, downstream_rid, operation, ts
		 FROM lineage_edges
		 WHERE upstream_rid = $1
		 ORDER BY ts DESC, id DESC
		 LIMIT $2`,
		upstreamRID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanLineageEdges(rows)
}

// scanLineageEdges drains a *pgx.Rows into a []LineageEdge. Caller closes
// rows; failures here are surfaced verbatim. Any pgx-specific type for
// rows is opaque here — we accept the Scan-capable interface so the helper
// stays simple and test-substitutable in future.
func scanLineageEdges(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]LineageEdge, error) {
	var out []LineageEdge
	for rows.Next() {
		var e LineageEdge
		var ts time.Time
		if err := rows.Scan(&e.ID, &e.UpstreamRID, &e.DownstreamRID, &e.Operation, &ts); err != nil {
			return nil, err
		}
		e.Timestamp = ts
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
