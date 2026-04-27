-- US-279 AIP Threads: persistent conversation threads with an LLM and
-- their ordered messages. Threads are owned by the user that created
-- them (created_by) and scoped to a single LLM provider; new providers
-- may be added by storing a different `provider` value (validated at
-- the API boundary by pkg/aip.IsKnownProvider).

CREATE TABLE IF NOT EXISTS aip_threads (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL DEFAULT '',
    provider     TEXT NOT NULL,
    model        TEXT NOT NULL DEFAULT '',
    system_prompt TEXT NOT NULL DEFAULT '',
    created_by   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS aip_threads_created_by_idx
    ON aip_threads(created_by, created_at DESC);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_threads_id_format') THEN
        ALTER TABLE aip_threads
            ADD CONSTRAINT aip_threads_id_format
            CHECK (id ~ '^[A-Za-z0-9._-]{1,128}$');
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_threads_provider_format') THEN
        ALTER TABLE aip_threads
            ADD CONSTRAINT aip_threads_provider_format
            CHECK (provider ~ '^[a-z][a-z0-9_-]{0,63}$');
    END IF;
END$$;

CREATE TABLE IF NOT EXISTS aip_messages (
    id           BIGSERIAL PRIMARY KEY,
    thread_id    TEXT NOT NULL REFERENCES aip_threads(id) ON DELETE CASCADE,
    role         TEXT NOT NULL,
    content      TEXT NOT NULL DEFAULT '',
    token_count  INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS aip_messages_thread_idx
    ON aip_messages(thread_id, id);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_messages_role_check') THEN
        ALTER TABLE aip_messages
            ADD CONSTRAINT aip_messages_role_check
            CHECK (role IN ('system', 'user', 'assistant'));
    END IF;
END$$;
