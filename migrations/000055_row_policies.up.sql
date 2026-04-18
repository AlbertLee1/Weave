-- US-256 Row-Level Security: store per-ObjectType predicate filters that are
-- AND-combined into the OSS query pipeline at read time. Predicate is a
-- where.WhereClause serialised as JSON; applies_to lists the roles, groups,
-- and user identifiers the policy governs. Multiple applicable policies are
-- OR-combined at compile time so admins can layer additive grants.

CREATE TABLE IF NOT EXISTS row_policies (
    rid              TEXT PRIMARY KEY,
    object_type_rid  TEXT NOT NULL REFERENCES object_types(rid) ON DELETE CASCADE,
    predicate        JSONB NOT NULL,
    applies_to       JSONB NOT NULL DEFAULT '{}'::jsonb,
    description      TEXT NOT NULL DEFAULT '',
    created_by       TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_row_policies_object_type ON row_policies(object_type_rid);
