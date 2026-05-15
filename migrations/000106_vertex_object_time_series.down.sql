-- VTX-028 reverse: drop cagg + table.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
        IF EXISTS (
            SELECT 1 FROM timescaledb_information.continuous_aggregates
            WHERE view_name = 'cagg_5min'
        ) THEN
            EXECUTE 'DROP MATERIALIZED VIEW IF EXISTS cagg_5min';
        END IF;
    END IF;
END$$;

DROP INDEX IF EXISTS object_time_series_object_property_ts_idx;
DROP TABLE IF EXISTS object_time_series;
