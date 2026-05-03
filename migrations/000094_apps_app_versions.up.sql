-- US-391 Apps + App Versions: Workshop-lite App Editor durable state.
-- The `apps` row is the live current-version record; `app_versions`
-- is the append-only history (one row per Update, version starts at
-- 1 on Create). The most-recent app_versions row matches the apps
-- columns by definition.
--
-- (owner_id, name) is unique so a user can't keep two Apps under the
-- same name; different owners may pick the same name without
-- colliding. version is monotonically-increasing per RID.

CREATE TABLE IF NOT EXISTS apps (
    rid         TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    owner_id    TEXT NOT NULL,
    layout_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    version     INT  NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'apps_name_format') THEN
        ALTER TABLE apps
            ADD CONSTRAINT apps_name_format
            CHECK (length(name) BETWEEN 1 AND 128);
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'apps_version_positive') THEN
        ALTER TABLE apps
            ADD CONSTRAINT apps_version_positive
            CHECK (version >= 1);
    END IF;
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS apps_owner_name_idx
    ON apps(owner_id, name);

CREATE INDEX IF NOT EXISTS apps_owner_idx
    ON apps(owner_id);

CREATE TABLE IF NOT EXISTS app_versions (
    app_rid     TEXT NOT NULL REFERENCES apps(rid) ON DELETE CASCADE,
    version     INT  NOT NULL,
    name        TEXT NOT NULL,
    layout_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by  TEXT NOT NULL,
    PRIMARY KEY (app_rid, version)
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'app_versions_version_positive') THEN
        ALTER TABLE app_versions
            ADD CONSTRAINT app_versions_version_positive
            CHECK (version >= 1);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS app_versions_rid_version_desc_idx
    ON app_versions(app_rid, version DESC);
