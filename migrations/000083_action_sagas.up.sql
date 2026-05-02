-- US-369 Action Saga: durable saga coordinator with multi-step
-- compensation, idempotency, and dead-letter queue.
--
-- action_sagas owns the saga envelope (idempotency_key UNIQUE, status
-- lifecycle RUNNING → SUCCESS | COMPENSATING → COMPENSATED | FAILED).
-- result_json snapshots the SagaResult JSON returned to the caller —
-- repeating the same idempotency_key reads this row back verbatim
-- instead of re-running the saga.
--
-- action_saga_steps is one row per declared step. action_type is the
-- API name; edits_json captures the primary-batch edits actually built
-- during prepare; inverse_edits_json captures the compensator's edits
-- when (and if) compensation runs. Per-step status mirrors the
-- envelope: PENDING → APPLIED | COMPENSATED | COMPENSATION_FAILED.
--
-- action_saga_dlq is the dead-letter queue for compensations that
-- failed during rollback. The retry handler reads PENDING rows and
-- replays the inverse edit batch; rows are kept indefinitely so an
-- operator can manually drop them after investigation.

CREATE TABLE IF NOT EXISTS action_sagas (
    saga_id          TEXT PRIMARY KEY,
    idempotency_key  TEXT,
    ontology         TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'RUNNING',
    requested_by     TEXT NOT NULL DEFAULT '',
    failure_message  TEXT NOT NULL DEFAULT '',
    result_json      JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_sagas_status_enum') THEN
        ALTER TABLE action_sagas
            ADD CONSTRAINT action_sagas_status_enum
            CHECK (status IN ('RUNNING', 'SUCCESS', 'COMPENSATING', 'COMPENSATED', 'FAILED'));
    END IF;
END$$;

CREATE UNIQUE INDEX IF NOT EXISTS action_sagas_idempotency_key_idx
    ON action_sagas(idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key <> '';

CREATE INDEX IF NOT EXISTS action_sagas_status_created_idx
    ON action_sagas(status, created_at);

CREATE TABLE IF NOT EXISTS action_saga_steps (
    step_id              TEXT PRIMARY KEY,
    saga_id              TEXT NOT NULL REFERENCES action_sagas(saga_id) ON DELETE CASCADE,
    step_index           INTEGER NOT NULL,
    action_type          TEXT NOT NULL,
    parameters           JSONB NOT NULL DEFAULT '{}'::jsonb,
    edits_json           JSONB,
    inverse_edits_json   JSONB,
    status               TEXT NOT NULL DEFAULT 'PENDING',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_saga_steps_status_enum') THEN
        ALTER TABLE action_saga_steps
            ADD CONSTRAINT action_saga_steps_status_enum
            CHECK (status IN ('PENDING', 'APPLIED', 'FAILED', 'COMPENSATED', 'COMPENSATION_FAILED'));
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS action_saga_steps_saga_idx
    ON action_saga_steps(saga_id, step_index);

CREATE TABLE IF NOT EXISTS action_saga_dlq (
    dlq_id          TEXT PRIMARY KEY,
    saga_id         TEXT NOT NULL REFERENCES action_sagas(saga_id) ON DELETE CASCADE,
    step_id         TEXT NOT NULL,
    ontology        TEXT NOT NULL,
    edits_json      JSONB NOT NULL DEFAULT '[]'::jsonb,
    failure_message TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'PENDING',
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_saga_dlq_status_enum') THEN
        ALTER TABLE action_saga_dlq
            ADD CONSTRAINT action_saga_dlq_status_enum
            CHECK (status IN ('PENDING', 'RESOLVED', 'DROPPED'));
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS action_saga_dlq_status_created_idx
    ON action_saga_dlq(status, created_at);
