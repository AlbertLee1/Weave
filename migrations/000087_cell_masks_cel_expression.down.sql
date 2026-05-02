DROP INDEX IF EXISTS idx_cell_masks_expression;

ALTER TABLE cell_masks
    DROP COLUMN IF EXISTS expression,
    DROP COLUMN IF EXISTS mask_strategy;
