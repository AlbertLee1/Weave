-- US-376 Cell-Level Masking CEL Expression Engine.
--
-- Extends US-258's cell_masks with two new columns:
--   * expression    — optional CEL expression (google/cel-go) evaluated per row
--                     against bindings {user.markings, user.id, user.email,
--                     user.roles, row.<property>}. When non-empty the expression
--                     decides "should this caller see this cell unmasked?"
--                     instead of (or in addition to) the legacy AppliesTo
--                     allow-list. Empty preserves the US-258 path.
--   * mask_strategy — REDACT | HASH | NULL | PARTIAL. Distinct from the legacy
--                     mask_rule (lowercase hash/redact/partial) which stays as
--                     the back-compat surface. When mask_strategy is non-empty
--                     it wins; when empty the engine falls back to mask_rule.
--                     The new uppercase wire shape matches the Foundry CEL
--                     mask-rule taxonomy and lets US-376 add NULL without
--                     breaking the older mask_rule consumers.

ALTER TABLE cell_masks
    ADD COLUMN IF NOT EXISTS expression    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS mask_strategy TEXT NOT NULL DEFAULT '';

-- Tighten lookup for "every CEL-expression mask on this object type" so the
-- per-request compile step in cellsec.Engine.Reload can short-circuit when
-- nothing has been authored. Partial index keeps it cheap on tables where
-- expression is overwhelmingly empty.
CREATE INDEX IF NOT EXISTS idx_cell_masks_expression
    ON cell_masks(object_type_rid)
    WHERE expression <> '';
