-- US-250: API key rotation (scheduled rotation + grace period).
--
-- rotates_at  : when set, the key is scheduled for automatic rotation; the
--               auth middleware treats the key as expired once now() crosses
--               this timestamp. During the grace window (rotates_at in the
--               future) both the predecessor key and its successor are
--               concurrently valid so callers can migrate without downtime.
-- successor_id: FK back into api_keys pointing at the freshly minted
--               replacement. Cleared on cascade so deleting the successor
--               never dangles. Population is one-shot: once set, the
--               predecessor is "in rotation" and cannot be rotated again.
--
-- No backfill: existing rows keep NULL for both columns, which reads as
-- "no rotation scheduled" and preserves legacy middleware behaviour.

ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS rotates_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS successor_id UUID REFERENCES api_keys(id) ON DELETE SET NULL;

-- Partial index: the rotation-warning queue scans active keys whose rotation
-- deadline is imminent; bounding the index on the non-NULL partition keeps
-- it small across the common "no rotation scheduled" case.
CREATE INDEX IF NOT EXISTS idx_api_keys_rotates_at
    ON api_keys (rotates_at)
    WHERE rotates_at IS NOT NULL AND revoked_at IS NULL;
