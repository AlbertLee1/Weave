-- US-258 Cell-Level Security: per-(ObjectType, primary_key, property) mask
-- transforms that rewrite a single cell's value during response serialisation.
-- Extends US-257's column_masks to the "one specific object instance + one
-- specific property" axis: column_masks target a whole (ObjectType, property)
-- column; cell_masks target a single cell.
--
-- mask_rule reuses the masking enum (hash|redact|partial). applies_to is the
-- ALLOWED identity list — callers matching applies_to see clear data, every
-- other caller receives the masked value (admins bypass via PermUserManage,
-- matching the column_masks convention).

CREATE TABLE IF NOT EXISTS cell_masks (
    rid               TEXT PRIMARY KEY,
    object_type_rid   TEXT NOT NULL REFERENCES object_types(rid) ON DELETE CASCADE,
    primary_key       TEXT NOT NULL,
    property_api_name TEXT NOT NULL,
    mask_rule         TEXT NOT NULL,
    applies_to        JSONB NOT NULL DEFAULT '{}'::jsonb,
    description       TEXT NOT NULL DEFAULT '',
    created_by        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cell_masks_object_type ON cell_masks(object_type_rid);
CREATE INDEX IF NOT EXISTS idx_cell_masks_lookup ON cell_masks(object_type_rid, primary_key);
