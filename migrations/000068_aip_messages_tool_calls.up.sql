-- US-284 AIP Function Calling Chain: extend aip_messages with the
-- tool-call fields needed to record an LLM function-calling loop.
--
-- New columns:
--   tool_calls    JSONB array — populated on assistant rows when the
--                 model requested tool invocations. NULL on regular
--                 assistant text responses (back-compat).
--   tool_call_id  Identifier of the assistant tool_call this row is
--                 the result of. Set on rows with role='tool'; empty
--                 on every other role.
--   tool_name     Name of the tool that produced the result. Set on
--                 rows with role='tool'; empty otherwise.
--
-- The role CHECK constraint is replaced (renamed) so role='tool' is
-- accepted alongside system/user/assistant. Existing rows are
-- unaffected (no value migration needed; new columns are nullable /
-- default-empty).

ALTER TABLE aip_messages
    ADD COLUMN IF NOT EXISTS tool_calls JSONB;

ALTER TABLE aip_messages
    ADD COLUMN IF NOT EXISTS tool_call_id TEXT NOT NULL DEFAULT '';

ALTER TABLE aip_messages
    ADD COLUMN IF NOT EXISTS tool_name TEXT NOT NULL DEFAULT '';

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_messages_role_check') THEN
        ALTER TABLE aip_messages DROP CONSTRAINT aip_messages_role_check;
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_messages_role_check_v2') THEN
        ALTER TABLE aip_messages
            ADD CONSTRAINT aip_messages_role_check_v2
            CHECK (role IN ('system', 'user', 'assistant', 'tool'));
    END IF;
END$$;
