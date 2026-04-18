-- US-212: Object Type Inheritance. ObjectType may now declare a single
-- parent ObjectType via `extends_rid`. The merge of parent properties /
-- outgoing links is performed at resolution time (pkg/oms.ResolveInheritance),
-- so the rest of the schema stays untouched — child rows still own their
-- direct properties; the resolver walks the chain on read.
--
-- The column is intentionally NULLable text without a self-referential FK
-- constraint: parent/child rows are authored separately, and inheritance is
-- validated in the admin handlers (existence + same-ontology + cycle check)
-- using the same pattern established by link_types.inverse_link_rid.
--
-- A partial index on extends_rid speeds up "list children of X" lookups,
-- which the resolver and validation paths use repeatedly.

ALTER TABLE object_types
    ADD COLUMN IF NOT EXISTS extends_rid TEXT;

CREATE INDEX IF NOT EXISTS ix_object_types_extends_rid
    ON object_types(extends_rid)
    WHERE extends_rid IS NOT NULL;
