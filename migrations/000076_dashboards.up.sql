-- US-329 Dashboards: per-user dashboard persistence with optional
-- public sharing. One row captures the dashboard's widget layout + any
-- display metadata as an opaque JSONB envelope (`definition`) so the
-- SPA can evolve the wire shape without a schema change.
--
-- (created_by, name) is unique so a user can't store two dashboards
-- under the same name; different users may pick the same name without
-- colliding. is_public toggles share-link semantics — when true, any
-- authenticated GET-by-id succeeds. Mutating routes stay owner-scoped
-- regardless of is_public.

CREATE TABLE IF NOT EXISTS dashboards (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL,
    created_by  TEXT NOT NULL,
    is_public   BOOLEAN NOT NULL DEFAULT FALSE,
    definition  JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'dashboards_name_format') THEN
        ALTER TABLE dashboards
            ADD CONSTRAINT dashboards_name_format
            CHECK (length(name) BETWEEN 1 AND 128);
    END IF;
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS dashboards_owner_name_idx
    ON dashboards(created_by, name);

CREATE INDEX IF NOT EXISTS dashboards_owner_idx
    ON dashboards(created_by);
