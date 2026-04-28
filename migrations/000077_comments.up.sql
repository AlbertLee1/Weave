-- US-334 Comments: per-RID threaded discussion persistence.
--
-- One row per comment authored against a target_rid (object, action
-- log, or any future commentable resource). Optional parent_id models
-- one-deep replies; depth is enforced at the application layer so the
-- Comments tab can render a flat two-level tree without recursion.
-- Soft delete via deleted_at — rows survive in place so reply chains
-- keep their parent reference and the audit trail stays intact. The
-- store layer redacts body on read for soft-deleted rows.

CREATE TABLE IF NOT EXISTS comments (
    id          UUID PRIMARY KEY,
    target_rid  TEXT NOT NULL,
    body        TEXT NOT NULL,
    author      TEXT NOT NULL,
    parent_id   UUID REFERENCES comments(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at  TIMESTAMPTZ
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'comments_body_length') THEN
        ALTER TABLE comments
            ADD CONSTRAINT comments_body_length
            CHECK (length(body) <= 8192);
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'comments_target_rid_format') THEN
        ALTER TABLE comments
            ADD CONSTRAINT comments_target_rid_format
            CHECK (target_rid LIKE 'ri.%');
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS comments_target_created_idx
    ON comments(target_rid, created_at);

CREATE INDEX IF NOT EXISTS comments_parent_idx
    ON comments(parent_id) WHERE parent_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS comments_author_idx
    ON comments(author);
