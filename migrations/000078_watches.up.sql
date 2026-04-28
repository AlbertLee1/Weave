-- US-337 Watches: per-user follow relationships.
--
-- One row records that a user wants to follow a target_rid (object,
-- action log, or any future watchable resource). A unique index on
-- (user_id, target_rid) enforces idempotent toggle semantics — calling
-- the create endpoint twice with the same pair surfaces the existing
-- row rather than producing a duplicate. The downstream activity
-- consumer (US-338) joins on this table to fan out change events to
-- every interested user.

CREATE TABLE IF NOT EXISTS watches (
    id          UUID PRIMARY KEY,
    user_id     TEXT NOT NULL,
    target_rid  TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'watches_target_rid_format') THEN
        ALTER TABLE watches
            ADD CONSTRAINT watches_target_rid_format
            CHECK (target_rid LIKE 'ri.%');
    END IF;
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS watches_user_target_idx
    ON watches(user_id, target_rid);

CREATE INDEX IF NOT EXISTS watches_target_idx
    ON watches(target_rid);
