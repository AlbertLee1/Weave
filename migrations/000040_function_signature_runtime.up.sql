-- US-215: Function Registry. Extends the existing functions table (introduced
-- in 000021) with the two columns the registry needs on top of the original
-- name+source_code shape:
--
--   signature - JSONB schema {params:[{name,type,required,default}], returns:{type}}
--               default '{}' so existing rows stay valid; admin handlers
--               validate shape at write time (params must be array, each
--               param has a name, returns has a type).
--   runtime   - 'goja' (default; embedded JS) or 'http' (delegate to URL).
--               CHECK constraint pins the enum at the DB layer; the model's
--               Validate() repeats the check at write time so the API surface
--               returns 400 instead of a generic insert failure.
--
-- The legacy column source_code is preserved as the universal "function body"
-- handle (Goja runs it as JS; for runtime=http it carries the endpoint URL).

ALTER TABLE functions
    ADD COLUMN IF NOT EXISTS signature JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE functions
    ADD COLUMN IF NOT EXISTS runtime TEXT NOT NULL DEFAULT 'goja';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'functions_runtime_check'
    ) THEN
        ALTER TABLE functions
            ADD CONSTRAINT functions_runtime_check
            CHECK (runtime IN ('goja', 'http'));
    END IF;
END$$;
