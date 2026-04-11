-- Foundry OSv2 TimeSeriesProperty storage.
--
-- Each row is one (time, value) reading on a series identified by
-- (ontology_rid, object_type, primary_key, property). value is JSONB so
-- numeric, string and struct payloads all round-trip unchanged.
CREATE TABLE IF NOT EXISTS timeseries_points (
    ontology_rid TEXT        NOT NULL,
    object_type  TEXT        NOT NULL,
    primary_key  TEXT        NOT NULL,
    property     TEXT        NOT NULL,
    ts           TIMESTAMPTZ NOT NULL,
    value        JSONB       NOT NULL,
    PRIMARY KEY (ontology_rid, object_type, primary_key, property, ts)
);

CREATE INDEX IF NOT EXISTS idx_timeseries_points_series_ts
    ON timeseries_points (ontology_rid, object_type, primary_key, property, ts);
