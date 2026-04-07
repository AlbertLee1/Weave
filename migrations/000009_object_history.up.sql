-- Object Change History (Tier 2.3): pre/post-state per-version timeline.
-- Each row records a single CREATE/MODIFY/DELETE applied to a specific
-- (object_type_rid, primary_key) tuple. prev_state and new_state are stored
-- as opaque JSONB so callers can render arbitrary diffs without joining
-- against bleve. Versions are monotonically increasing per primary key.

CREATE TABLE object_history (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    object_type_rid TEXT NOT NULL,
    primary_key     TEXT NOT NULL,
    version         BIGINT NOT NULL,
    prev_state      JSONB,
    new_state       JSONB,
    edit_type       TEXT NOT NULL CHECK (edit_type IN ('CREATE', 'MODIFY', 'DELETE')),
    action_log_rid  TEXT,
    user_id         TEXT,
    recorded_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_object_history_lookup ON object_history (object_type_rid, primary_key, version DESC);
CREATE INDEX idx_object_history_time ON object_history (recorded_at DESC);
