// VTX-028 — Foundry-Vertex window-aggregation query (PG/TimescaleDB).
//
// The PG-bound entrypoint of VertexService lives here so the unit-test
// coverage gate (covercheck excludeFiles) can drop it alongside the
// other integration-only files (pg_store.go, pg_repo.go, ...). The
// surrounding struct, options, and pure SQL helpers (aggSQL,
// pgInterval) stay in vertex_service.go and are unit-tested without a
// database.

package timeseries

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Query returns a window-aggregated series for (ObjectRID, Property) over
// [From, To) bucketed by Bucket. When a ScenarioID + overlay are present
// and the overlay supplies a windowed-scalar override, every returned
// bucket carries that scalar. When the last observation on the series is
// older than the configured warning threshold relative to To, the result
// carries Warning="missing_data" and LastObservedAt set to the latest ts
// in the table.
func (s *VertexService) Query(ctx context.Context, q VertexQuery) (*VertexQueryResult, error) {
	if q.Bucket <= 0 {
		return nil, fmt.Errorf("timeseries: Bucket must be > 0")
	}
	if !q.To.After(q.From) {
		return nil, fmt.Errorf("timeseries: To must be after From")
	}

	aggExpr, err := aggSQL(q.Agg)
	if err != nil {
		return nil, err
	}

	// Use TimescaleDB time_bucket — every supported Agg is parameterised as
	// the same SQL shape so we route through one prepared statement.
	sql := fmt.Sprintf(
		`SELECT time_bucket($1::INTERVAL, ts) AS bucket, %s AS value
		   FROM object_time_series
		  WHERE object_rid = $2 AND property = $3 AND ts >= $4 AND ts < $5
		  GROUP BY bucket
		  ORDER BY bucket ASC`, aggExpr)
	interval := pgInterval(q.Bucket)

	rows, err := s.pool.Query(ctx, sql, interval, q.ObjectRID, q.Property, q.From, q.To)
	if err != nil {
		return nil, fmt.Errorf("timeseries: query: %w", err)
	}
	defer rows.Close()

	out := &VertexQueryResult{Points: []BucketedPoint{}}
	for rows.Next() {
		var p BucketedPoint
		if err := rows.Scan(&p.Time, &p.Value); err != nil {
			return nil, fmt.Errorf("timeseries: scan: %w", err)
		}
		out.Points = append(out.Points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Apply scenario overlay (windowed scalar replaces every bucket).
	if q.ScenarioID != "" && s.overlay != nil {
		override, err := s.overlay.GetWindowedScalarOverride(ctx, q.ScenarioID, q.ObjectRID, q.Property)
		if err != nil {
			return nil, err
		}
		if override != nil {
			for i := range out.Points {
				out.Points[i].Value = *override
			}
		}
	}

	// Missing-data warning: compare latest observation against To.
	if s.missingDataWarningHours > 0 {
		var last time.Time
		err := s.pool.QueryRow(ctx,
			`SELECT MAX(ts) FROM object_time_series WHERE object_rid = $1 AND property = $2`,
			q.ObjectRID, q.Property).Scan(&last)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("timeseries: last-observed query: %w", err)
		}
		if !last.IsZero() {
			gap := q.To.Sub(last)
			if gap > time.Duration(s.missingDataWarningHours)*time.Hour {
				out.Warning = "missing_data"
				lo := last
				out.LastObservedAt = &lo
			}
		}
	}

	return out, nil
}
