-- US-200: Derived Properties framework. Introduces two columns on the
-- properties table so an ObjectType author can mark a property as a
-- query-time computed expression rather than stored state.
--
--   derived  BOOLEAN  - when true, the value of this property is computed
--                       from `formula` at query time and is never written
--                       through Action edits or read from a datasource.
--   formula  TEXT     - JavaScript expression evaluated by the sandboxed
--                       Goja runtime (see pkg/types/formula). `this`
--                       refers to the already-loaded object property map.
--
-- Both default to safe/no-op values so existing rows need no backfill.

ALTER TABLE properties
    ADD COLUMN IF NOT EXISTS derived BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS formula TEXT NOT NULL DEFAULT '';
