-- VTX-010: Vertex graph LinkType classes.
--
-- Each LinkType may carry a small set of behavioural tags that Vertex graph
-- rendering uses to choose edge arrow style:
--   - vertex:link_primary_direction
--   - vertex:link_undirectional
--   - vertex:link_bidirectional
--
-- Stored as a TEXT[] (PostgreSQL array). NULL/empty array means "no tags"
-- and preserves the pre-VTX-010 wire shape (the column is omitted from
-- JSON output via omitempty).

ALTER TABLE link_types
    ADD COLUMN IF NOT EXISTS type_classes TEXT[] NOT NULL DEFAULT '{}';
