-- Weave M2M link edges: shared junction table.
-- Each row represents a single many-to-many edge between two objects.
-- See .omc/scientist/reports/20260406_024032_m2m_reverse_traversal_design.md
-- for rationale: single shared table vs. per-link tables.
CREATE TABLE link_edges (
    link_type_rid     TEXT NOT NULL REFERENCES link_types(rid) ON DELETE CASCADE,
    source_object_rid TEXT NOT NULL,
    target_object_rid TEXT NOT NULL,
    edge_properties   JSONB,
    created_at        TIMESTAMPTZ DEFAULT now(),
    PRIMARY KEY (link_type_rid, source_object_rid, target_object_rid)
);

-- Forward index: source -> targets (link_type_rid, source_object_rid).
CREATE INDEX idx_link_edges_fwd ON link_edges (link_type_rid, source_object_rid);
-- Reverse index: target -> sources (link_type_rid, target_object_rid).
CREATE INDEX idx_link_edges_rev ON link_edges (link_type_rid, target_object_rid);
