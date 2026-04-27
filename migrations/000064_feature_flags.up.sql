-- US-276 Feature Flags: dynamic per-realm / per-user rollout gating.
--
-- One row per flag. enabled is the primary kill-switch; realms / users
-- narrow the rollout to a subset of callers when they are non-empty.
-- See pkg/featureflags/flag.go for the evaluation rules.

CREATE TABLE IF NOT EXISTS feature_flags (
    name         TEXT PRIMARY KEY,
    description  TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT FALSE,
    realms       TEXT[] NOT NULL DEFAULT '{}',
    users        TEXT[] NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'feature_flags_name_format') THEN
        ALTER TABLE feature_flags
            ADD CONSTRAINT feature_flags_name_format
            CHECK (name ~ '^[A-Za-z0-9._-]{1,128}$');
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS feature_flags_enabled_idx
    ON feature_flags(enabled)
    WHERE enabled;
