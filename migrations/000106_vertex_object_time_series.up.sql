-- VTX-028 Vertex Object Time Series: hypertable + 5-min continuous aggregate.
--
-- Stores per-(object, property) numeric time series points. The base table
-- is plain PostgreSQL so the migration stays applicable to the pgvector dev
-- image; when TimescaleDB is available (timescale/timescaledb-ha:pg16) the
-- DO block below registers the hypertable and the cagg_5min continuous
-- aggregate. Local `make docker-up` users running pgvector get the table
-- (write/read still works as a normal Postgres table); production and the
-- VTX-028 integration tests run on the TimescaleDB image and exercise the
-- hypertable + cagg performance paths.

CREATE TABLE IF NOT EXISTS object_time_series (
    object_rid TEXT             NOT NULL,
    property   TEXT             NOT NULL,
    ts         TIMESTAMPTZ      NOT NULL,
    value      DOUBLE PRECISION NOT NULL,
    quality    SMALLINT         NOT NULL DEFAULT 0,
    PRIMARY KEY (object_rid, property, ts)
);

CREATE INDEX IF NOT EXISTS object_time_series_object_property_ts_idx
    ON object_time_series (object_rid, property, ts DESC);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb') THEN
        CREATE EXTENSION IF NOT EXISTS timescaledb;
        -- Promote to hypertable. migrate_data => TRUE keeps any rows
        -- already present (no-op on fresh db); if_not_exists guards against
        -- re-runs.
        PERFORM create_hypertable(
            'object_time_series',
            'ts',
            chunk_time_interval => INTERVAL '7 days',
            migrate_data        => TRUE,
            if_not_exists       => TRUE
        );

        -- 5-minute continuous aggregate: AVG / MIN / MAX / LAST / SUM /
        -- COUNT per (object_rid, property, 5-min bucket). Foundry OSv2
        -- Quiver queries hit this view; VTX-029 Time Series Service falls
        -- through to the raw table when bucket < 5 min.
        IF NOT EXISTS (
            SELECT 1 FROM timescaledb_information.continuous_aggregates
            WHERE view_name = 'cagg_5min'
        ) THEN
            EXECUTE $cagg$
                CREATE MATERIALIZED VIEW cagg_5min
                WITH (timescaledb.continuous) AS
                SELECT
                    object_rid,
                    property,
                    time_bucket(INTERVAL '5 minutes', ts) AS bucket,
                    AVG(value)   AS avg_value,
                    MIN(value)   AS min_value,
                    MAX(value)   AS max_value,
                    SUM(value)   AS sum_value,
                    COUNT(value) AS count_value
                FROM object_time_series
                GROUP BY object_rid, property, bucket
                WITH NO DATA
            $cagg$;
        END IF;
    END IF;
END$$;
