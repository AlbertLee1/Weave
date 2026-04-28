-- US-342 Reactions: per-(user, target_rid, emoji) toggle.
--
-- One row records that a user has reacted with a particular emoji to a
-- target_rid (object, comment, action log, or any future reactable
-- resource). The unique index on (user_id, target_rid, emoji) enforces
-- idempotent toggle semantics — calling the create endpoint twice with
-- the same triple surfaces the existing row instead of producing a
-- duplicate. Aggregate counts come from a single GROUP BY emoji query
-- backed by the (target_rid, emoji) index.

CREATE TABLE IF NOT EXISTS reactions (
    id          UUID PRIMARY KEY,
    user_id     TEXT NOT NULL,
    target_rid  TEXT NOT NULL,
    emoji       TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'reactions_target_rid_format') THEN
        ALTER TABLE reactions
            ADD CONSTRAINT reactions_target_rid_format
            CHECK (target_rid LIKE 'ri.%');
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'reactions_emoji_length') THEN
        ALTER TABLE reactions
            ADD CONSTRAINT reactions_emoji_length
            CHECK (length(emoji) BETWEEN 1 AND 32);
    END IF;
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS reactions_user_target_emoji_idx
    ON reactions(user_id, target_rid, emoji);

CREATE INDEX IF NOT EXISTS reactions_target_emoji_idx
    ON reactions(target_rid, emoji);
