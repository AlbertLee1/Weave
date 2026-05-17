-- US-487 Row-Level Security CEL gates: add cel_expression column to the
-- row_policies table so admin-create surfaces can persist CEL gates
-- alongside the legacy WhereClause predicate. The two columns are
-- independent enforcement lanes:
--   predicate       — WhereClause JSON compiled into a Bleve disjunct
--   cel_expression  — CEL source string evaluated per-row in OSS
-- A policy must populate at least one; both is allowed (CEL acts as a
-- strict additional filter on top of the Bleve-side predicate). The
-- existing predicate NOT NULL constraint is relaxed so CEL-only rows
-- can land — handler-level Validate still enforces "one or the other".
ALTER TABLE row_policies
    ADD COLUMN IF NOT EXISTS cel_expression TEXT NOT NULL DEFAULT '';

ALTER TABLE row_policies
    ALTER COLUMN predicate DROP NOT NULL;
