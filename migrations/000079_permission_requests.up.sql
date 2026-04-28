-- US-339 Permission Requests: share-link access workflow.
--
-- One row records that a user (requested_by) is asking for access to
-- target_rid because the share link they received is gated. An approver
-- (admin / ontology-owner / any future "approver" role) reviews the row
-- and transitions it to APPROVED or REJECTED, optionally with a
-- decision_note. The terminal states are final — re-decide attempts
-- surface 409 Conflict.
--
-- Notification fan-out to approvers (on create) and to the requester
-- (on decision) is handled by pkg/permissionrequests via the existing
-- OMS notifications table; this migration only owns the request rows.

CREATE TABLE IF NOT EXISTS permission_requests (
    id             UUID PRIMARY KEY,
    target_rid     TEXT NOT NULL,
    requested_by   TEXT NOT NULL,
    reason         TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'PENDING',
    decided_by     TEXT NOT NULL DEFAULT '',
    decision_note  TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at     TIMESTAMPTZ
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'permission_requests_target_rid_format') THEN
        ALTER TABLE permission_requests
            ADD CONSTRAINT permission_requests_target_rid_format
            CHECK (target_rid LIKE 'ri.%');
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'permission_requests_status_enum') THEN
        ALTER TABLE permission_requests
            ADD CONSTRAINT permission_requests_status_enum
            CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED'));
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'permission_requests_reason_length') THEN
        ALTER TABLE permission_requests
            ADD CONSTRAINT permission_requests_reason_length
            CHECK (length(reason) <= 4096 AND length(decision_note) <= 4096);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS permission_requests_status_created_idx
    ON permission_requests(status, created_at);

CREATE INDEX IF NOT EXISTS permission_requests_requester_idx
    ON permission_requests(requested_by, created_at);

CREATE INDEX IF NOT EXISTS permission_requests_target_idx
    ON permission_requests(target_rid, status);
