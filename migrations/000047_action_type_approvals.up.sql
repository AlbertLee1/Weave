-- US-242: Approval Workflow.
-- Adds approval-gating metadata to action_types (requires_approval flag +
-- approvers array of role names / user IDs) and a new action_approvals table
-- that records pending / terminal approval requests. The request lifecycle is
-- PENDING → (APPROVED | REJECTED); terminal rows are immutable via the
-- handler layer (409 Conflict on re-approve). Parameters is a JSONB snapshot
-- of the original apply body so the approver can review what will run if the
-- request is approved.

ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS requires_approval BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS approvers JSONB NOT NULL DEFAULT '[]'::jsonb;

-- Partial index keeps the "gated actions only" lookup cheap.
CREATE INDEX IF NOT EXISTS action_types_requires_approval_idx
    ON action_types(requires_approval)
    WHERE requires_approval = TRUE;

CREATE TABLE IF NOT EXISTS action_approvals (
    id                 TEXT PRIMARY KEY,
    action_type_rid    TEXT NOT NULL,
    ontology_api_name  TEXT NOT NULL,
    action_type        TEXT NOT NULL,
    parameters         JSONB NOT NULL DEFAULT '{}'::jsonb,
    approvers          JSONB NOT NULL DEFAULT '[]'::jsonb,
    status             TEXT NOT NULL DEFAULT 'PENDING',
    requested_by       TEXT NOT NULL DEFAULT '',
    reviewed_by        TEXT NOT NULL DEFAULT '',
    reason             TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Pending queue lookups key off ontology + status; admin reviews key off
-- reviewer identity.
CREATE INDEX IF NOT EXISTS action_approvals_pending_idx
    ON action_approvals(ontology_api_name, status, created_at DESC);

CREATE INDEX IF NOT EXISTS action_approvals_requested_by_idx
    ON action_approvals(requested_by)
    WHERE requested_by <> '';

-- Status enum. Wrapped in DO $$ so the idempotent re-apply path (migrate-down
-- then migrate-up) doesn't choke on a duplicate-constraint error. Prior art:
-- migrations/000046 action_jobs_status_enum.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'action_approvals_status_enum') THEN
        ALTER TABLE action_approvals
            ADD CONSTRAINT action_approvals_status_enum
            CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED'));
    END IF;
END$$;
