-- PRD-V2 Gap-A4 follow-up: persist round-31's SideEffectOutcome[] to
-- action_logs so Foundry-style action history can show "webhook 1/3
-- succeeded on 2nd attempt, webhook 2/3 gave up after 3 attempts".
--
-- side_effect_status is a JSONB array of objects shaped like:
--   [{type, status, attempts, error, durationMs}, ...]
-- where status ∈ {success, failed, non_retryable, unknown_type}.
--
-- NULL means either (a) the row pre-dates this migration or (b) the
-- action had zero side effects. Either way, callers treat NULL as
-- "no side-effect outcomes available" and render an empty status
-- column in the UI.
ALTER TABLE action_logs ADD COLUMN IF NOT EXISTS side_effect_status JSONB DEFAULT NULL;

COMMENT ON COLUMN action_logs.side_effect_status IS
  'JSONB array of per-effect dispatch outcomes (PRD-V2 Gap-A4). One object per declared SideEffect with shape {type, status, attempts, error, durationMs}. NULL means the action had no side effects or the row pre-dates 2026-05-24.';
