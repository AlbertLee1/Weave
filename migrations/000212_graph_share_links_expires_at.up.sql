-- Add optional expiry timestamps to live Vertex graph share links.
--
-- A NULL expires_at preserves existing indefinite links. Non-NULL values are
-- enforced by graphsvc at the boundary: now >= expires_at returns 410 Gone.

ALTER TABLE graph_share_links
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS graph_share_links_expires_at_idx
    ON graph_share_links(expires_at)
    WHERE expires_at IS NOT NULL;
