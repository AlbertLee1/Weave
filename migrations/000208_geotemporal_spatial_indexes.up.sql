-- US-466: GeoTemporal PG persistence — spatial+temporal index hardening.
--
-- Migration 000205 created the geotemporal_values table with a single
-- composite btree on (ontology, object_type, primary_key, property, ts).
-- That index already gives O(log n) lookups for a fully-specified series
-- plus ts range, but it does NOT help bbox-shaped queries: the planner
-- still has to fetch every row in the time window and re-evaluate the
-- JSONB coordinates filter row-by-row.
--
-- This migration adds three btree indexes the planner can BitmapAnd with
-- the series-key index to prune the spatial axis, plus an optional PostGIS
-- GIST index that only materialises if the postgis extension is installed
-- (the default pgvector image in CI does NOT have postgis, so we MUST be
-- a no-op there — failing the migration would break every integration
-- test).
--
-- Index naming follows the convention of migration 000205:
--   idx_geotemporal_values_<axis>.

-- Standalone btree on ts. Useful for cross-series time-window scans
-- (e.g. "all readings in the last hour, any object") that the composite
-- (series_keys, ts) index can't satisfy because the leading columns are
-- unbound.
CREATE INDEX IF NOT EXISTS idx_geotemporal_values_ts
    ON geotemporal_values (ts);

-- Functional indexes on the extracted longitude/latitude. Casting once
-- inside the expression index means the planner can use the indexes for
-- bbox queries without parsing each JSONB payload at scan time.
CREATE INDEX IF NOT EXISTS idx_geotemporal_values_lng
    ON geotemporal_values (((position->'coordinates'->>0)::float8));

CREATE INDEX IF NOT EXISTS idx_geotemporal_values_lat
    ON geotemporal_values (((position->'coordinates'->>1)::float8));

-- Optional PostGIS GIST index. Wrapped in a DO block so the migration is
-- a no-op on databases that don't have the postgis extension installed,
-- which is the case for both the default development image (postgres:16)
-- and the testcontainers fixture (pgvector/pgvector:pg16). Operators who
-- want true spatial-index performance can install postgis upstream and
-- re-run this migration (idempotent — CREATE INDEX IF NOT EXISTS).
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'postgis') THEN
        EXECUTE 'CREATE INDEX IF NOT EXISTS idx_geotemporal_values_geom '
             || 'ON geotemporal_values USING GIST ('
             || '    ST_SetSRID(ST_MakePoint('
             || '        ((position->''coordinates''->>0)::float8),'
             || '        ((position->''coordinates''->>1)::float8)'
             || '    ), 4326)'
             || ')';
    END IF;
END
$$;
