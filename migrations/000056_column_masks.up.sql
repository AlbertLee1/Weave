-- US-257 Column-Level Masking: per-(ObjectType, property) mask transforms
-- that rewrite property values during response serialisation. mask_rule is
-- one of hash|redact|partial; applies_to lists the roles, groups, and user
-- identifiers the mask governs. Masks apply ONLY to non-matching callers:
-- callers that fall inside applies_to receive the unmasked value (i.e.
-- AppliesTo carries the "allowed" identities, NOT the "masked" ones). An
-- empty applies_to means the mask applies to everyone (including admins,
-- unless they carry the bypass permission which the engine checks for).

CREATE TABLE IF NOT EXISTS column_masks (
    rid              TEXT PRIMARY KEY,
    object_type_rid  TEXT NOT NULL REFERENCES object_types(rid) ON DELETE CASCADE,
    property_api_name TEXT NOT NULL,
    mask_rule        TEXT NOT NULL,
    applies_to       JSONB NOT NULL DEFAULT '{}'::jsonb,
    description      TEXT NOT NULL DEFAULT '',
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_column_masks_object_type ON column_masks(object_type_rid);
