-- Round 64: extend permission_requests.status enum to allow CANCELLED.
--
-- Round 63 added the requester-side soft-cancel surface (DELETE
-- /api/v2/permission-requests/{id}) and a fourth StatusCancelled =
-- "CANCELLED" terminal state. MemoryStore-backed tests pass, but the
-- PG-backed deployment path enforces a CHECK constraint —
-- permission_requests_status_enum — that pins status to the original
-- three values (PENDING / APPROVED / REJECTED). Without this
-- migration, the first real cancel call against PG fails with
-- 23514 check_violation and the row stays PENDING.
--
-- The constraint is dropped and re-added (rather than ALTER-ing in
-- place) because PG doesn't support "extend an enum" on a
-- CHECK-with-IN constraint — only on native enum types, which the
-- original migration deliberately avoided for portability.
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'permission_requests_status_enum') THEN
        ALTER TABLE permission_requests DROP CONSTRAINT permission_requests_status_enum;
    END IF;
END$$;

ALTER TABLE permission_requests
    ADD CONSTRAINT permission_requests_status_enum
    CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'CANCELLED'));
