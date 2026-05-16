-- OSV2-301: GeoTemporal series PostgreSQL persistence.
--
-- Each row is one (time, position) reading on a geotemporal series identified
-- by (ontology, object_type, primary_key, property). Position is stored as
-- JSONB so GeoJSON Point payloads round-trip unchanged — no PostGIS required.
CREATE TABLE IF NOT EXISTS geotemporal_values (
    ontology     TEXT        NOT NULL,
    object_type  TEXT        NOT NULL,
    primary_key  TEXT        NOT NULL,
    property     TEXT        NOT NULL,
    ts           TIMESTAMPTZ NOT NULL,
    position     JSONB       NOT NULL,
    PRIMARY KEY (ontology, object_type, primary_key, property, ts)
);

CREATE INDEX IF NOT EXISTS idx_geotemporal_values_series_ts
    ON geotemporal_values (ontology, object_type, primary_key, property, ts);
