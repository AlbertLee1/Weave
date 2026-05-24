-- PRD-V2 Gap-A4 final: persist failed-after-retries side-effect
-- dispatches to a dead-letter queue so an operator can review and
-- replay them. Round 30 added the retry loop; round 31 introduced
-- SideEffectOutcome; round 32 stamps outcomes onto action_logs;
-- round 33 (this migration) routes the FAILED outcomes to a
-- durable queue.
--
-- One row per failed effect dispatch. action_log_id + effect_index
-- jointly identify which effect on which action attempt failed.
-- effect_config carries the original SideEffect.Config blob so a
-- future replay handler can dispatch without reading the action
-- type definition again (action types can be updated between the
-- original dispatch and a manual replay).
--
-- outcome carries the full SideEffectOutcome (type, status,
-- attempts, error, durationMs) so the admin UI can render
-- "webhook X failed after 4 attempts: ...".
--
-- replay_status drives the admin workflow:
--   - pending   — never replayed; default for newly-inserted rows
--   - replayed  — operator manually replayed via the admin API
--                 (round 34) and the dispatcher succeeded
--   - abandoned — operator declared this irrecoverable
CREATE TABLE IF NOT EXISTS action_log_side_effect_dlq (
    id              BIGSERIAL PRIMARY KEY,
    action_log_id   BIGINT NOT NULL REFERENCES action_logs(id) ON DELETE CASCADE,
    effect_index    INT NOT NULL,
    effect_type     TEXT NOT NULL,
    effect_config   JSONB NOT NULL DEFAULT 'null'::jsonb,
    outcome         JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    replay_status   TEXT NOT NULL DEFAULT 'pending',
    replayed_at     TIMESTAMPTZ,
    replay_count    INT NOT NULL DEFAULT 0,

    CONSTRAINT action_log_side_effect_dlq_unique_per_action
        UNIQUE (action_log_id, effect_index)
);

CREATE INDEX IF NOT EXISTS idx_action_log_side_effect_dlq_action_log_id
    ON action_log_side_effect_dlq(action_log_id);

CREATE INDEX IF NOT EXISTS idx_action_log_side_effect_dlq_pending
    ON action_log_side_effect_dlq(replay_status, created_at DESC)
    WHERE replay_status = 'pending';

COMMENT ON TABLE action_log_side_effect_dlq IS
    'Failed-after-retries side-effect dispatches (PRD-V2 Gap-A4 round 33). One row per (action_log_id, effect_index) pair when the round-30 retry loop gave up. replay_status drives the admin replay workflow (round 34).';
