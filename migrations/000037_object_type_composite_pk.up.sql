-- US-211: Composite Primary Keys. ObjectType may now declare more than one
-- property as its primary key (e.g. orderId + lineNumber). The legacy
-- single-key column `primary_key_prop TEXT` stays put so older code paths
-- and existing rows keep working unchanged; a parallel JSONB array column
-- carries the full composite list and is what new code reads/writes.
--
-- Backfill rule: every existing row gets a single-element array containing
-- its current primary_key_prop, so single-PK lookups continue to round-trip
-- identically through either column.
--
-- The route format /objects/{objectType}/{key1}:{key2} treats the URL
-- segment as opaque to chi — only the handler splits on `:`, gated on the
-- ObjectType declaring len(PrimaryKeys) > 1. Single-PK ObjectTypes treat
-- the full URL segment as the literal key (so legacy PKs containing `:`
-- still resolve).

ALTER TABLE object_types
    ADD COLUMN IF NOT EXISTS primary_key_props JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE object_types
SET primary_key_props = jsonb_build_array(primary_key_prop)
WHERE primary_key_props = '[]'::jsonb
  AND primary_key_prop IS NOT NULL
  AND primary_key_prop <> '';
