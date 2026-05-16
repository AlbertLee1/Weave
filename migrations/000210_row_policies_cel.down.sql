-- Reverse US-487 CEL gate column. The cel_expression column is dropped
-- in full; restoring NOT NULL on predicate would fail if any CEL-only
-- rows survive, so the down migration leaves predicate NULL-able. Re-
-- applying 000055 separately is the only way back to the original
-- pre-US-487 constraint shape.
ALTER TABLE row_policies
    DROP COLUMN IF EXISTS cel_expression;
