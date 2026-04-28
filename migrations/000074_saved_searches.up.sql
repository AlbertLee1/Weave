-- US-311 Saved Searches: per-user named query persistence.
--
-- One row per saved search. (created_by, name) is unique so a user can't
-- have two searches under the same name; different users may pick the same
-- name without colliding. The wire-shape JSON definition (search text,
-- filters, facets, sort) lives in `definition` JSONB so the front-end can
-- evolve the saved-view payload without a schema change. The (ontology,
-- object_type) tuple is denormalised onto its own columns so List can be
-- scoped to the browser-page tab the user is currently viewing.

CREATE TABLE IF NOT EXISTS saved_searches (
    id           UUID PRIMARY KEY,
    name         TEXT NOT NULL,
    ontology     TEXT NOT NULL,
    object_type  TEXT NOT NULL,
    created_by   TEXT NOT NULL,
    definition   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'saved_searches_name_format') THEN
        ALTER TABLE saved_searches
            ADD CONSTRAINT saved_searches_name_format
            CHECK (length(name) BETWEEN 1 AND 128);
    END IF;
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS saved_searches_owner_name_idx
    ON saved_searches(created_by, name);

CREATE INDEX IF NOT EXISTS saved_searches_owner_scope_idx
    ON saved_searches(created_by, ontology, object_type);
