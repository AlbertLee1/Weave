DROP INDEX IF EXISTS action_approvals_requested_by_idx;
DROP INDEX IF EXISTS action_approvals_pending_idx;
DROP TABLE IF EXISTS action_approvals;

DROP INDEX IF EXISTS action_types_requires_approval_idx;
ALTER TABLE action_types
    DROP COLUMN IF EXISTS approvers,
    DROP COLUMN IF EXISTS requires_approval;
