-- US-025: Mark individual properties as "edit-only" so user edits on them
-- always win regardless of the active conflict-resolution strategy. Pairs
-- with the funnel consumer always-apply path landed in US-027.
-- Existing rows default to false (no behaviour change).

ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS is_edit_only BOOLEAN NOT NULL DEFAULT FALSE;
