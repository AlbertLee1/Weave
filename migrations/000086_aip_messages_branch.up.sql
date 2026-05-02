-- US-374: AIP Threads fork & branch.
--
-- Extend aip_messages with the two columns the PRD calls for so a thread's
-- message history can be modelled as a forest rather than a flat list:
--   - parent_message_id BIGINT — references aip_messages.id of the
--                                immediately preceding message on the
--                                same branch. NULL for the first message
--                                of a thread / fork (the root).
--   - branch_id          TEXT  — short branch identifier. Defaults to
--                                'main' on legacy rows. New forks created
--                                via POST /aip/threads/{id}/fork allocate
--                                a fresh thread row with branch_id='main'
--                                so a thread is itself one branch tree.
--
-- The (thread_id, branch_id, id) order remains the canonical linear
-- read order — branch_id lets future in-place forking (US-375) keep
-- multiple branches inside the same thread without breaking ListMessages
-- ordering. parent_message_id is the structural backbone the
-- /tree endpoint walks to render the message forest.

ALTER TABLE aip_messages
    ADD COLUMN IF NOT EXISTS parent_message_id BIGINT
        REFERENCES aip_messages(id) ON DELETE SET NULL;

ALTER TABLE aip_messages
    ADD COLUMN IF NOT EXISTS branch_id TEXT NOT NULL DEFAULT 'main';

CREATE INDEX IF NOT EXISTS aip_messages_parent_idx
    ON aip_messages(parent_message_id);

CREATE INDEX IF NOT EXISTS aip_messages_thread_branch_idx
    ON aip_messages(thread_id, branch_id, id);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_messages_branch_id_format') THEN
        ALTER TABLE aip_messages
            ADD CONSTRAINT aip_messages_branch_id_format
            CHECK (branch_id ~ '^[A-Za-z0-9._-]{1,128}$');
    END IF;
END$$;
