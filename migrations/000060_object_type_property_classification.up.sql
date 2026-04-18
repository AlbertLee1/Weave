-- US-262: data-classification metadata on ObjectTypes and Properties.
--
-- Admins tag each ObjectType / Property with an optional label from a fixed
-- vocabulary (Public / Internal / Confidential / PII / Secret). The column
-- is nullable with no default: existing rows come back as "unspecified"
-- without any backfill, matching the pre-US-262 behaviour. Empty-string
-- normalisation happens at the Go layer via `NULLIF($N, '')` on INSERT /
-- `COALESCE(col, '')` on SELECT so the model stays a plain `string`.
--
-- Enforcement (search / export policy gating) is out of scope for US-262;
-- the column is metadata-only in v1 and will be consumed by the existing
-- marking / masking engines in a follow-up story.

ALTER TABLE object_types
    ADD COLUMN IF NOT EXISTS classification TEXT;

ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS classification TEXT;
