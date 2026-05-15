-- VTX-013 SystemGraph share links.
--
-- A graph_share_links row carries an opaque random token a recipient can
-- exchange for a masked view of a system_graphs row. Revocation is a soft
-- flag so future reads can return 410 Gone (distinguishing from a 404 on a
-- token that never existed).

CREATE TABLE IF NOT EXISTS graph_share_links (
    token       TEXT PRIMARY KEY,
    graph_rid   TEXT NOT NULL REFERENCES system_graphs(rid) ON DELETE CASCADE,
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked     BOOLEAN NOT NULL DEFAULT FALSE,
    revoked_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS graph_share_links_graph_idx
    ON graph_share_links(graph_rid);
