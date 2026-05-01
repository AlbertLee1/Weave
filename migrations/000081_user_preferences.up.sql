-- US-350 User Preferences Center: per-user persisted settings.
--
-- One row per authenticated user. The four wire-shape JSON envelopes
-- (theme, language, notifications, hotkeys) are intentionally narrow
-- columns + opaque JSONB so the SPA can evolve the per-section payload
-- without a schema change.
--
-- theme/language are TEXT (small enum-like) instead of nested JSONB so
-- common SQL queries ("how many users on en?") stay cheap. Empty string
-- means "no preference" — the SPA falls back to its localStorage / OS
-- defaults the first time the row is created.

CREATE TABLE IF NOT EXISTS user_preferences (
    user_id        TEXT PRIMARY KEY,
    theme          TEXT NOT NULL DEFAULT '',
    language       TEXT NOT NULL DEFAULT '',
    notifications  JSONB NOT NULL DEFAULT '{}'::jsonb,
    hotkeys        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'user_preferences_theme_format') THEN
        ALTER TABLE user_preferences
            ADD CONSTRAINT user_preferences_theme_format
            CHECK (theme IN ('', 'dark', 'light', 'system'));
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'user_preferences_language_format') THEN
        ALTER TABLE user_preferences
            ADD CONSTRAINT user_preferences_language_format
            CHECK (length(language) <= 32);
    END IF;
END$$;
