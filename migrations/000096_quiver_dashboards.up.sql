-- US-403 Quiver dashboards: persist a Quiver workbench (series spec list,
-- color choices, optional saved selection range) as a read-only dashboard
-- so analysts can share a snapshot of an analysis. The wire shape is
-- opaque JSONB so the SPA can evolve `config_json` (panel layout, axis
-- ranges, transform chain) without a schema change.
--
-- (owner, name) is unique per-owner so the same analyst can't store two
-- dashboards under the same name; different analysts may pick the same
-- name. Read-only sharing is RID-based: any authenticated caller may GET
-- /quiver/{rid}/view; mutating routes (save, delete) stay owner-scoped.

CREATE TABLE IF NOT EXISTS quiver_dashboards (
    rid         TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    owner       TEXT NOT NULL,
    config_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'quiver_dashboards_name_format') THEN
        ALTER TABLE quiver_dashboards
            ADD CONSTRAINT quiver_dashboards_name_format
            CHECK (length(name) BETWEEN 1 AND 128);
    END IF;
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS quiver_dashboards_owner_name_idx
    ON quiver_dashboards(owner, name);

CREATE INDEX IF NOT EXISTS quiver_dashboards_owner_idx
    ON quiver_dashboards(owner);
