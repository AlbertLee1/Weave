-- US-223 Time-Travel Queries: bitemporal validity range columns on
-- object_history. valid_from is the timestamp at which the row's new_state
-- becomes the "current" state for (object_type_rid, primary_key); valid_to
-- is the timestamp at which it ceases to be current (i.e. when the next
-- version lands). The latest live row carries valid_to = NULL.
--
-- A snapshot at time T for a PK is the unique row satisfying:
--     valid_from <= T AND (valid_to IS NULL OR valid_to > T)
-- DELETE rows participate so callers can detect "did not exist at T" by
-- treating edit_type='DELETE' as a tombstone.

ALTER TABLE object_history
    ADD COLUMN IF NOT EXISTS valid_from TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS valid_to   TIMESTAMPTZ;

-- Backfill valid_from from recorded_at, and valid_to from the next version's
-- recorded_at (NULL for the latest version per PK). The DO block lets us
-- re-run the migration after a partial apply without re-doing the work.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM object_history
         WHERE valid_from IS NULL
            OR (valid_to IS NULL AND id IN (
                SELECT id FROM (
                    SELECT id, version,
                           LEAD(version) OVER (
                               PARTITION BY object_type_rid, primary_key
                               ORDER BY version
                           ) AS next_version
                      FROM object_history
                ) t WHERE t.next_version IS NOT NULL
            ))
    ) THEN
        UPDATE object_history h
        SET valid_from = COALESCE(h.valid_from, h.recorded_at),
            valid_to   = nxt.next_recorded
        FROM (
            SELECT id,
                   LEAD(recorded_at) OVER (
                       PARTITION BY object_type_rid, primary_key
                       ORDER BY version
                   ) AS next_recorded
              FROM object_history
        ) AS nxt
        WHERE h.id = nxt.id;
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS idx_object_history_valid_range
    ON object_history (object_type_rid, valid_from, valid_to);
