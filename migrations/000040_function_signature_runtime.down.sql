ALTER TABLE functions
    DROP CONSTRAINT IF EXISTS functions_runtime_check;

ALTER TABLE functions
    DROP COLUMN IF EXISTS runtime;

ALTER TABLE functions
    DROP COLUMN IF EXISTS signature;
