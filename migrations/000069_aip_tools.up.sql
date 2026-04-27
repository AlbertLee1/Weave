-- US-285 LLM Tool 扩展: persist custom AIP tools the LLM may invoke
-- during a SendMessage function-calling loop. Each row defines one
-- tool: the LLM-visible (name, description, parameters) triple plus
-- the optional handler_function_rid that points at a Function in the
-- Function Registry (US-089). At boot the cmd/server wiring loads
-- every enabled row into the aip.ToolRegistry so the SendMessage
-- handler from US-284 sees them alongside the built-in echo tool.
--
-- name             unique, LLM-visible identifier (matches the JSON
--                  schema "name" in OpenAI / Anthropic tool defs)
-- description      free-form description shown to the LLM
-- parameters       JSON Schema object for the tool arguments — handed
--                  verbatim to the provider via ChatRequest.Tools
-- handler_function_rid
--                  RID of a Function (pkg/oms.Function) that
--                  implements the tool. Empty string means the tool
--                  has no server-side handler yet (LLM may still see
--                  the def but Execute returns an unconfigured error).
-- enabled          when false the tool is hidden from the registry
-- created_by       tracks the authoring user

CREATE TABLE IF NOT EXISTS aip_tools (
    name                 TEXT PRIMARY KEY,
    description          TEXT NOT NULL DEFAULT '',
    parameters           JSONB NOT NULL DEFAULT '{}'::jsonb,
    handler_function_rid TEXT NOT NULL DEFAULT '',
    enabled              BOOLEAN NOT NULL DEFAULT TRUE,
    created_by           TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS aip_tools_enabled_idx
    ON aip_tools(enabled);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'aip_tools_name_format') THEN
        ALTER TABLE aip_tools
            ADD CONSTRAINT aip_tools_name_format
            CHECK (name ~ '^[A-Za-z][A-Za-z0-9_]{0,63}$');
    END IF;
END$$;
