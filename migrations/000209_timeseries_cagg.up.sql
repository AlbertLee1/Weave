-- US-467 TimeSeries 5-minute continuous aggregate.
--
-- timeseries_points (migrations/000016) keeps every raw (ts, value JSONB)
-- reading on a series. For the SDK-facing DownsamplePoints path we want a
-- materialised 5-minute summary so a >=5min-aligned avg/sum/min/max/count
-- query can re-aggregate the cagg's ~288 buckets/day per series instead of
-- scanning a million raw rows.
--
-- When TimescaleDB is available (timescale/timescaledb:latest-pg16 image
-- used by `internal/testutil.StartTimescaleDBContainer`):
--   1. Promote timeseries_points to a hypertable on `ts` (idempotent).
--   2. Create the timeseries_cagg_5min continuous aggregate keyed by the
--      full (ontology_rid, object_type, primary_key, property, bucket)
--      composite. avg/sum/min/max/count are materialised; first/last are
--      not (the cagg can't store the latest ts cheaply, so DownsamplePoints
--      routes those queries to the raw table via the `last(value, ts)` and
--      `first(value, ts)` hyperfunctions).
--   3. Register a pg_cron `*/5 * * * *` refresh when pg_cron is present;
--      cmd/server runs an app-side ticker as a redundant fallback (US-467).
--
-- On a plain pgvector image the entire DO block is a no-op so the rest of
-- the integration suite keeps passing.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb') THEN
        CREATE EXTENSION IF NOT EXISTS timescaledb;

        IF NOT EXISTS (
            SELECT 1 FROM timescaledb_information.hypertables
             WHERE hypertable_name = 'timeseries_points'
        ) THEN
            PERFORM create_hypertable(
                'timeseries_points',
                'ts',
                chunk_time_interval => INTERVAL '7 days',
                migrate_data        => TRUE,
                if_not_exists       => TRUE
            );
        END IF;

        IF NOT EXISTS (
            SELECT 1 FROM timescaledb_information.continuous_aggregates
             WHERE view_name = 'timeseries_cagg_5min'
        ) THEN
            EXECUTE $cagg$
                CREATE MATERIALIZED VIEW timeseries_cagg_5min
                WITH (timescaledb.continuous) AS
                SELECT
                    ontology_rid,
                    object_type,
                    primary_key,
                    property,
                    time_bucket(INTERVAL '5 minutes', ts) AS bucket,
                    AVG((value::text)::float8)            AS avg_value,
                    MIN((value::text)::float8)            AS min_value,
                    MAX((value::text)::float8)            AS max_value,
                    SUM((value::text)::float8)            AS sum_value,
                    COUNT(value)                          AS count_value
                FROM timeseries_points
                WHERE jsonb_typeof(value) = 'number'
                GROUP BY ontology_rid, object_type, primary_key, property, bucket
                WITH NO DATA
            $cagg$;
        END IF;

        -- pg_cron schedule when the extension is available; the app-side
        -- ticker (RunCAGGRefreshLoop in pkg/timeseries/pg_store.go) keeps
        -- the cagg fresh on databases without pg_cron.
        IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'pg_cron') THEN
            CREATE EXTENSION IF NOT EXISTS pg_cron;
            PERFORM cron.schedule(
                'timeseries-cagg-5min-refresh',
                '*/5 * * * *',
                $cmd$ CALL refresh_continuous_aggregate('timeseries_cagg_5min', NULL, NULL); $cmd$
            );
        END IF;
    END IF;
END$$;
