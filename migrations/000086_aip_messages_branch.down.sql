-- US-374 down: drop fork/branch columns from aip_messages.

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_messages_branch_id_format') THEN
        ALTER TABLE aip_messages DROP CONSTRAINT aip_messages_branch_id_format;
    END IF;
END$$;

DROP INDEX IF EXISTS aip_messages_thread_branch_idx;
DROP INDEX IF EXISTS aip_messages_parent_idx;

ALTER TABLE aip_messages DROP COLUMN IF EXISTS branch_id;
ALTER TABLE aip_messages DROP COLUMN IF EXISTS parent_message_id;
