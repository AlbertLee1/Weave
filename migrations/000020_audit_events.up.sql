-- US-064: Audit events table for recording OMS metadata changes, auth events,
-- and security policy mutations with actor, resource, and diff context.

CREATE TABLE IF NOT EXISTS audit_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id    TEXT        NOT NULL,
    action      TEXT        NOT NULL,
    resource_type TEXT      NOT NULL,
    resource_rid  TEXT      NOT NULL,
    diff_json   JSONB,
    ip          TEXT        NOT NULL DEFAULT '',
    user_agent  TEXT        NOT NULL DEFAULT '',
    ts          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_events_actor    ON audit_events (actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_events_action   ON audit_events (action);
CREATE INDEX IF NOT EXISTS idx_audit_events_resource ON audit_events (resource_type, resource_rid);
CREATE INDEX IF NOT EXISTS idx_audit_events_ts       ON audit_events (ts DESC);
