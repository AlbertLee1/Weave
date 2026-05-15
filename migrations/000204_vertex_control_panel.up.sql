-- VTX-015 Vertex Control Panel — admin-tunable runtime configuration.
--
-- Single-row table (id = 1) holding the operator-tunable knobs the Vertex
-- workspace consults: default time window, polling interval, search-around
-- bounds, missing-data warning threshold. Schema mirrors the JSON wire shape;
-- defaults match VTX-015 BDD acceptance verbatim.

CREATE TABLE IF NOT EXISTS vertex_control_panel (
    id                          INTEGER PRIMARY KEY CHECK (id = 1),
    default_window_days         INTEGER NOT NULL DEFAULT 30,
    polling_interval_sec        INTEGER NOT NULL DEFAULT 5,
    search_around_max_nodes     INTEGER NOT NULL DEFAULT 200,
    search_around_max_depth     INTEGER NOT NULL DEFAULT 3,
    missing_data_warning_hours  INTEGER NOT NULL DEFAULT 24,
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
