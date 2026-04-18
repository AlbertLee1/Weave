-- Reverse US-200 derived/formula columns.

ALTER TABLE properties DROP COLUMN IF EXISTS formula;
ALTER TABLE properties DROP COLUMN IF EXISTS derived;
