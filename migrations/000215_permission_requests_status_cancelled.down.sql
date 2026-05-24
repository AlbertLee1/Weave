-- Down: revert the status enum to its pre-round-63 shape.
--
-- Any rows currently carrying CANCELLED would violate the narrower
-- constraint, so we first rewrite them to REJECTED (the closest
-- semantic neighbour — both are terminal "didn't grant access"
-- states). DecidedBy / DecidedAt are preserved so the audit trail
-- survives the down-migration even though the "who said no" answer
-- changes from "requester" to "system on schema rollback".
UPDATE permission_requests
   SET status = 'REJECTED',
       decision_note = CASE
           WHEN decision_note = '' THEN 'auto-rewritten from CANCELLED on schema rollback (migration 000215 down)'
           ELSE decision_note || ' [auto-rewritten from CANCELLED on rollback]'
       END,
       updated_at = NOW()
 WHERE status = 'CANCELLED';

DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'permission_requests_status_enum') THEN
        ALTER TABLE permission_requests DROP CONSTRAINT permission_requests_status_enum;
    END IF;
END$$;

ALTER TABLE permission_requests
    ADD CONSTRAINT permission_requests_status_enum
    CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED'));
