-- Reverse US-284 — drop the function-calling-chain columns and roll
-- back to the original system/user/assistant role enum.

ALTER TABLE aip_messages DROP COLUMN IF EXISTS tool_calls;
ALTER TABLE aip_messages DROP COLUMN IF EXISTS tool_call_id;
ALTER TABLE aip_messages DROP COLUMN IF EXISTS tool_name;

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_messages_role_check_v2') THEN
        ALTER TABLE aip_messages DROP CONSTRAINT aip_messages_role_check_v2;
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_messages_role_check') THEN
        ALTER TABLE aip_messages
            ADD CONSTRAINT aip_messages_role_check
            CHECK (role IN ('system', 'user', 'assistant'));
    END IF;
END$$;
