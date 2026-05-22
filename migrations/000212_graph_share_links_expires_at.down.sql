DROP INDEX IF EXISTS graph_share_links_expires_at_idx;

ALTER TABLE graph_share_links
    DROP COLUMN IF EXISTS expires_at;
