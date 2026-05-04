-- US-412: Track .weavepkg ontology archives that have been installed via the
-- pkg install API. The marketplace UI (US-413, US-454) lists rows from this
-- table and toggles the `enabled` flag for soft-disable, while uninstall
-- deletes the row outright.
CREATE TABLE IF NOT EXISTS installed_packages (
    id            BIGSERIAL PRIMARY KEY,
    name          TEXT NOT NULL,
    version       TEXT NOT NULL,
    ontology      TEXT NOT NULL,
    manifest_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    migrations    TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    enabled       BOOLEAN NOT NULL DEFAULT TRUE,
    installed_by  TEXT NOT NULL DEFAULT '',
    installed_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT installed_packages_name_unique UNIQUE (name)
);

CREATE INDEX IF NOT EXISTS idx_installed_packages_ontology
    ON installed_packages (ontology);

CREATE INDEX IF NOT EXISTS idx_installed_packages_enabled
    ON installed_packages (enabled)
    WHERE enabled = TRUE;
