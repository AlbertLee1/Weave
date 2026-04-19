-- US-267 GDPR Data Erasure.
--
-- Two tables backstop the right-to-be-forgotten flow:
--
--   gdpr_erasure_jobs  — async job state for POST /api/admin/gdpr/erase.
--                       Mirrors the action_jobs (US-240) shape so pollers
--                       see the same {status, progress, result, error_message}
--                       envelope. Steps array (JSONB) records per-step
--                       progress so operators can see WHICH step failed when
--                       a job ends in FAILED.
--
--   gdpr_redactions    — overlay table that flags an actor_id as redacted.
--                       The audit chain (US-266 hash-linked) is intentionally
--                       NOT mutated — that would break entry_hash linkage
--                       end-to-end. Instead, the audit List path joins this
--                       table and rewrites actor_id / ip / user_agent /
--                       diff_json to redacted sentinels for any matching
--                       row. Original chain stays verifiable; PII is
--                       unrecoverable from the API surface.

CREATE TABLE IF NOT EXISTS gdpr_erasure_jobs (
    job_id         TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'PENDING',
    progress       INTEGER NOT NULL DEFAULT 0,
    steps          JSONB NOT NULL DEFAULT '[]'::jsonb,
    error_message  TEXT NOT NULL DEFAULT '',
    requested_by   TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS gdpr_erasure_jobs_user_idx
    ON gdpr_erasure_jobs(user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS gdpr_erasure_jobs_requested_by_idx
    ON gdpr_erasure_jobs(requested_by)
    WHERE requested_by <> '';

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'gdpr_erasure_jobs_progress_range') THEN
        ALTER TABLE gdpr_erasure_jobs
            ADD CONSTRAINT gdpr_erasure_jobs_progress_range
            CHECK (progress >= 0 AND progress <= 100);
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'gdpr_erasure_jobs_status_enum') THEN
        ALTER TABLE gdpr_erasure_jobs
            ADD CONSTRAINT gdpr_erasure_jobs_status_enum
            CHECK (status IN ('PENDING', 'RUNNING', 'SUCCEEDED', 'FAILED'));
    END IF;
END$$;

CREATE TABLE IF NOT EXISTS gdpr_redactions (
    actor_id     TEXT PRIMARY KEY,
    reason       TEXT NOT NULL DEFAULT 'gdpr_erase',
    redacted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
