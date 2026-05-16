-- US-467 reverse: unschedule, drop the cagg, leave timeseries_points alone.
-- The hypertable promotion in the up migration is irreversible without
-- data motion, so the down only removes the materialised view + pg_cron
-- entry; a fresh `up` is idempotent.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_cron') THEN
        PERFORM cron.unschedule(jobid)
          FROM cron.job WHERE jobname = 'timeseries-cagg-5min-refresh';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'timescaledb') THEN
        IF EXISTS (
            SELECT 1 FROM timescaledb_information.continuous_aggregates
             WHERE view_name = 'timeseries_cagg_5min'
        ) THEN
            EXECUTE 'DROP MATERIALIZED VIEW IF EXISTS timeseries_cagg_5min';
        END IF;
    END IF;
END$$;
